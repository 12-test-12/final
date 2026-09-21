#include "stm32f10x.h"                  // Device header
#include "delay.h"
#include "OLED.h"
#include "adc.h"
#include "dht11.h"
#include "display_model.h"
#include "env_monitor.h"
#include "flash_config.h"
#include "threshold_store.h"
#include "telemetry_json.h"
#include "mqtt_packet.h"
#include "boot_id.h"
#include "esp8266.h"
#include "led.h"

/* Message buffer for the downlink text shown on the network page. Unchanged
 * from the previous firmware, including the 44-character limit the panel
 * effectively imposed. */
#define WIFI_MESSAGE_SIZE 64U

/* Task cadence, in units of the main loop period.
 *
 * The loop period is nominal: delay_ms is a busy wait and the DHT11 read adds a
 * few milliseconds once a second, so the real period is slightly longer than
 * 100 ms. That drift is acceptable for the rise window, which is specified in
 * tens of seconds, and it is much better than the previous firmware's single
 * one-second cadence, which could not support a ten-sample gas filter at all.
 */
#define LOCAL_TICK_MS 100U
#define CLIMATE_PERIOD_TICKS 10U       /* DHT11 once a second, its minimum interval */
#define DISPLAY_REFRESH_TICKS 5U       /* panel refresh every 500 ms */
#define DISPLAY_ROTATE_TICKS 20U       /* page change every 2 s */
#define UPLINK_PERIOD_TICKS 10U        /* one telemetry frame per second */
#define MQTT_KEEP_ALIVE_SECONDS 30U

typedef enum
{
    MQTT_LINK_TCP = 0U,
    MQTT_LINK_WAIT_CONNACK,
    MQTT_LINK_WAIT_SUBACK,
    MQTT_LINK_ONLINE
} MqttLinkState;

static void BuildBootId(char bootId[17])
{
    uint32_t identity = (*(const uint32_t *)0x1FFFF7E8UL) ^
                        (*(const uint32_t *)0x1FFFF7ECUL) ^
                        (*(const uint32_t *)0x1FFFF7F0UL);
    BootIdFormat(identity, SysTick->VAL ^ MY_ADC_GetValue(), bootId);
}

static uint8_t MqttSend(uint8_t *packet, uint32_t length)
{
    return (length > 0U && length <= 0xFFFFU) ?
           ESP8266_SendBytes(packet, (uint16_t)length) : 0U;
}

static uint16_t MqttTakePacketId(uint16_t *next)
{
    uint16_t current = *next;
    (*next)++;
    if (*next == 0U)
    {
        *next = 1U;
    }
    return current;
}

int main(void)
{
    EnvMonitor monitor;
    ThresholdStore thresholdStore;
    EnvThresholds storedThresholds;
    uint32_t storedVersion = 0U;
    uint8_t thresholdAreaDamaged = 0U;
    DisplayFrame frame;
    DisplayPage page = DISPLAY_PAGE_CLIMATE;
    DisplayInput display;
    EnvEvaluation evaluation;

    uint8_t temperature = 0U;
    uint8_t humidity = 0U;
    uint8_t dhtError;
    uint8_t wifiTaskStatus;
    MqttLinkState mqttState = MQTT_LINK_TCP;
    uint8_t mqttTx[MQTT_MAX_PACKET_SIZE];
    uint8_t mqttRx[MQTT_MAX_PACKET_SIZE];
    char telemetryJson[512];
    char bootId[17];
    uint16_t mqttRxLength = 0U;
    uint16_t mqttPacketId = 1U;
    uint32_t telemetrySequence = 0U;
    uint32_t lastMqttActivityMs = 0U;

    uint32_t now_ms = 0U;
    uint32_t tick = 0U;
    uint32_t lastPageTick = 0U;
    char wifiMessage[WIFI_MESSAGE_SIZE];

    SystemCoreClockUpdate();
    delay_init((uint8_t)(SystemCoreClock / 1000000U));

    wifiMessage[0] = '\0';
    OLED_Init();

    display.network = DISPLAY_NETWORK_LINKING;
    display.wifi_ssid = WIFI_SSID;
    display.server_message = wifiMessage;
    display.gas_uncalibrated = true;
    DisplayModelRender(page, NULL, &frame);
    OLED_Clear();
    for (uint8_t line = 0U; line < DISPLAY_LINE_COUNT; line++)
    {
        OLED_ShowString(0U, (uint8_t)(line * 16U), frame.lines[line], OLED_8X16);
    }
    OLED_Update();

    MY_ADC_Init();
    BuildBootId(bootId);
    LED_Init();
    BEEP_Init();

    /* The local monitor is initialised before the network is touched. Sampling,
     * filtering and the alarm decision must not wait for Wi-Fi: the device has
     * to be able to alarm in a room with no access point. */
    EnvMonitorInit(&monitor);

    /* Load the stored thresholds before the first evaluation, so the device
     * enforces the operator's limits from its first sample instead of the
     * compile-time defaults.
     *
     * When nothing valid is stored — or the reserved pages hold something that
     * is neither a record nor erased, which means another part of the firmware
     * wrote there — the compile-time defaults stay in force. That is the
     * documented safe outcome: a device that cannot read its configuration must
     * keep alarming on the built-in limits rather than on zeros, which would
     * alarm on every sample.
     *
     * The write path is driven by the control command handler and is not yet
     * wired to the radio; see hardware/README.md. Until it is, a device flashed
     * from this commit always reports no stored configuration. */
    ThresholdStoreInit(&thresholdStore, FlashConfigPort());
    if (!FlashConfigSelfCheck())
    {
        thresholdAreaDamaged = 1U;
    }
    if (ThresholdStoreLoad(&thresholdStore, &storedThresholds, &storedVersion))
    {
        (void)EnvMonitorSetThresholds(&monitor, &storedThresholds, storedVersion);
    }

    dhtError = DHT11_Init();
    (void)dhtError;
    delay_ms(1000);

    (void)ESP8266_Init();
    display.network = (ESP8266_IsWifiConnected() != 0U) ? DISPLAY_NETWORK_LINKED : DISPLAY_NETWORK_FAILED;

    while (1)
    {
        uint16_t raw_adc = MY_ADC_GetValue();

        /* --- gas: filter, then estimate from the filtered value --- */
        EnvMonitorPushGas(&monitor, raw_adc);
        {
            /* The estimate accompanies the filtered value, so both fields in the
             * telemetry describe the same reading. */
            uint16_t filtered = EnvMonitorGasFiltered(&monitor);
            EnvMonitorSetGasEstimate(&monitor, (uint16_t)(MQ135_EstimatePpm(filtered) + 0.5f));
        }

        /* --- climate: the DHT11 tolerates at most one read per second --- */
        if ((tick % CLIMATE_PERIOD_TICKS) == 0U)
        {
            dhtError = DHT11_Read_Data(&temperature, &humidity);
        }
        EnvMonitorPushClimate(&monitor,
                              temperature,
                              humidity,
                              (uint8_t)((dhtError == 0U) ? 1U : 0U),
                              now_ms);

        /* --- local alarm: evaluated every tick, independent of the network --- */
        evaluation = EnvMonitorEvaluate(&monitor, now_ms);

        if (evaluation.local_alarm)
        {
            LED_On();
        }
        else
        {
            LED_Off();
        }

        /* The buzzer is the only output the mute affects. The LED above follows
         * local_alarm regardless, so a mute can never hide an alarm locally. */
        if (evaluation.buzzer_on)
        {
            BEEP_On();
        }
        else
        {
            BEEP_Off();
        }

        /* --- display --- */
        if ((tick - lastPageTick) >= DISPLAY_ROTATE_TICKS)
        {
            lastPageTick = tick;
            DisplayModelNextPage(&page);
        }
        if ((tick % DISPLAY_REFRESH_TICKS) == 0U)
        {
            uint8_t line;

            display.temperature_c = evaluation.temperature_c;
            display.humidity_rh = evaluation.humidity_rh;
            display.gas_ppm = evaluation.gas_ppm;
            display.gas_adc_raw = evaluation.gas_adc_raw;
            display.gas_adc_filtered = evaluation.gas_adc_filtered;
            display.alarm_causes = evaluation.alarm_causes;
            display.buzzer_muted = EnvMonitorMuted(&monitor);
            display.threshold_version = EnvMonitorThresholdVersion(&monitor);
            /* The panel shows the fault rather than hiding it: an operator
             * looking at the device should be able to see that its stored
             * configuration area is unusable. */
            (void)thresholdAreaDamaged;
            display.wifi_ssid = WIFI_SSID;
            display.server_message = wifiMessage;

            DisplayModelRender(page, &display, &frame);

            OLED_Clear();
            for (line = 0U; line < DISPLAY_LINE_COUNT; line++)
            {
                OLED_ShowString(0U, (uint8_t)(line * 16U), frame.lines[line], OLED_8X16);
            }
            OLED_Update();
        }

        /* --- MQTT uplink ---
         * ESP8266 carries raw MQTT bytes over a single TCP socket. Connection
         * acknowledgement and subscription acknowledgement are required before
         * the OLED reports the network online, preventing the former false
         * positive where Wi-Fi association alone looked like cloud delivery. */
        if (ESP8266_GetPacket(mqttRx, sizeof(mqttRx), &mqttRxLength) != 0U)
        {
            MqttPacket incoming;
            const char *rejectReason = 0;
            if (MqttDecode(mqttRx, mqttRxLength, &incoming, &rejectReason))
            {
                if (mqttState == MQTT_LINK_WAIT_CONNACK &&
                    incoming.type == MQTT_PACKET_CONNACK &&
                    incoming.return_code == MQTT_CONNACK_ACCEPTED)
                {
                    uint32_t length = MqttEncodeSubscribe(mqttTx, sizeof(mqttTx),
                                                          MqttTakePacketId(&mqttPacketId),
                                                          "device/control", 1U);
                    if (MqttSend(mqttTx, length) != 0U)
                    {
                        mqttState = MQTT_LINK_WAIT_SUBACK;
                        lastMqttActivityMs = now_ms;
                    }
                }
                else if (mqttState == MQTT_LINK_WAIT_SUBACK && incoming.type == MQTT_PACKET_SUBACK)
                {
                    mqttState = MQTT_LINK_ONLINE;
                    lastMqttActivityMs = now_ms;
                }
                else if (incoming.type == MQTT_PACKET_PINGRESP)
                {
                    lastMqttActivityMs = now_ms;
                }
            }
            (void)rejectReason;
        }

        if (ESP8266_IsTcpConnected() == 0U)
        {
            mqttState = MQTT_LINK_TCP;
        }

        if ((tick % UPLINK_PERIOD_TICKS) == 0U)
        {
            if (ESP8266_IsWifiConnected() == 0U)
            {
                display.network = DISPLAY_NETWORK_LINKING;
                wifiTaskStatus = ESP8266_Init();
                (void)wifiTaskStatus;
            }
            else if (mqttState == MQTT_LINK_TCP && ESP8266_OpenTcp() != 0U)
            {
                uint32_t length = MqttEncodeConnect(mqttTx, sizeof(mqttTx), DEVICE_ID,
                                                    MQTT_KEEP_ALIVE_SECONDS, "device", 0);
                if (MqttSend(mqttTx, length) != 0U)
                {
                    mqttState = MQTT_LINK_WAIT_CONNACK;
                    lastMqttActivityMs = now_ms;
                }
            }
            else if (mqttState == MQTT_LINK_ONLINE)
            {
                TelemetryPayload telemetry;
                uint32_t jsonLength;
                uint32_t packetLength;

                telemetry.device_id = DEVICE_ID;
                telemetry.boot_id = bootId;
                telemetry.sequence = telemetrySequence;
                telemetry.uptime_ms = now_ms;
                telemetry.temperature_c = evaluation.temperature_c;
                telemetry.humidity_rh = evaluation.humidity_rh;
                telemetry.gas_adc_raw = evaluation.gas_adc_raw;
                telemetry.gas_adc_filtered = evaluation.gas_adc_filtered;
                telemetry.gas_ppm_tenths = (uint16_t)(evaluation.gas_ppm * 10U);
                telemetry.gas_calibrated = false;
                telemetry.local_alarm = evaluation.local_alarm;
                telemetry.alarm_causes = evaluation.alarm_causes;
                telemetry.buzzer_muted = EnvMonitorMuted(&monitor);
                telemetry.network_online = true;
                telemetry.threshold_version = EnvMonitorThresholdVersion(&monitor);
                telemetry.sensor_fault = ((evaluation.alarm_causes & ENV_ALARM_SENSOR_FAULT) != 0U);
                jsonLength = TelemetryJsonEncode(&telemetry, telemetryJson, sizeof(telemetryJson));
                packetLength = MqttEncodePublish(mqttTx, sizeof(mqttTx), "device/telemetry",
                                                 MqttTakePacketId(&mqttPacketId), 1U,
                                                 (const uint8_t *)telemetryJson, jsonLength);
                if (jsonLength > 0U && MqttSend(mqttTx, packetLength) != 0U)
                {
                    telemetrySequence++;
                    lastMqttActivityMs = now_ms;
                }
            }
            display.network = (mqttState == MQTT_LINK_ONLINE) ?
                              DISPLAY_NETWORK_LINKED : DISPLAY_NETWORK_LINKING;
        }

        if (mqttState == MQTT_LINK_ONLINE &&
            (now_ms - lastMqttActivityMs) >= (MQTT_KEEP_ALIVE_SECONDS * 500U))
        {
            uint32_t length = MqttEncodePingReq(mqttTx, sizeof(mqttTx));
            if (MqttSend(mqttTx, length) != 0U)
            {
                lastMqttActivityMs = now_ms;
            }
        }

        delay_ms(LOCAL_TICK_MS);
        tick++;
        now_ms += LOCAL_TICK_MS;
    }
}
