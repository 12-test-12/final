#include "stm32f10x.h"                  // Device header
#include "delay.h"
#include "OLED.h"
#include "adc.h"
#include "dht11.h"
#include "display_model.h"
#include "env_monitor.h"
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

int main(void)
{
    EnvMonitor monitor;
    DisplayFrame frame;
    DisplayPage page = DISPLAY_PAGE_CLIMATE;
    DisplayInput display;
    EnvEvaluation evaluation;

    uint8_t temperature = 0U;
    uint8_t humidity = 0U;
    uint8_t dhtError;
    uint8_t wifiTaskStatus;

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
    LED_Init();
    BEEP_Init();

    /* The local monitor is initialised before the network is touched. Sampling,
     * filtering and the alarm decision must not wait for Wi-Fi: the device has
     * to be able to alarm in a room with no access point. */
    EnvMonitorInit(&monitor);

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

        /* --- uplink --- */
        if ((tick % UPLINK_PERIOD_TICKS) == 0U)
        {
            ESP8266_SetData(evaluation.gas_ppm, evaluation.temperature_c, evaluation.humidity_rh);
            wifiTaskStatus = ESP8266_Task();
            if (wifiTaskStatus == ESP8266_STATUS_RECONNECT_REQUIRED)
            {
                display.network = DISPLAY_NETWORK_LINKING;
                (void)ESP8266_Init();
            }
            (void)ESP8266_GetMessage(wifiMessage, sizeof(wifiMessage));
            display.network = (ESP8266_IsWifiConnected() != 0U) ?
                              DISPLAY_NETWORK_LINKED : DISPLAY_NETWORK_FAILED;
        }

        delay_ms(LOCAL_TICK_MS);
        tick++;
        now_ms += LOCAL_TICK_MS;
    }
}
