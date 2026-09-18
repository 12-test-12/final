#include "stm32f10x.h"                  // Device header
#include "delay.h"
#include "OLED.h"
#include "adc.h"
#include "dht11.h"
#include "esp8266.h"
#include "led.h"
#include "app_config.h"

#define WIFI_MESSAGE_SIZE 64U

typedef enum
{
	WIFI_DISPLAY_LINKING = 0U,
	WIFI_DISPLAY_LINKED,
	WIFI_DISPLAY_FAILED
} WifiDisplayState;

static void OLED_ShowMessage(const char *message, WifiDisplayState wifiState)
{
	char line[17];
	uint8_t lineNumber;
	uint8_t sourceIndex = 0U;

	OLED_Clear();
	if (wifiState == WIFI_DISPLAY_LINKED)
	{
		OLED_ShowString(0, 0, "Linked:", OLED_6X8);
		OLED_ShowString(42, 0, WIFI_SSID, OLED_6X8);
	}
	else if (wifiState == WIFI_DISPLAY_LINKING)
	{
		OLED_ShowString(0, 0, "Linking:", OLED_6X8);
		OLED_ShowString(48, 0, WIFI_SSID, OLED_6X8);
	}
	else
	{
		OLED_ShowString(0, 0, "Link failed", OLED_6X8);
	}

	for (lineNumber = 0U; lineNumber < 3U; lineNumber++)
	{
		uint8_t column = 0U;

		if (lineNumber == 0U)
		{
			line[column++] = 'm';
			line[column++] = 's';
			line[column++] = 'g';
			line[column++] = ':';
		}

		while ((column < 16U) && (message[sourceIndex] != '\0'))
		{
			line[column] = message[sourceIndex];
			column++;
			sourceIndex++;
		}
		line[column] = '\0';

		OLED_ShowString(0,
		                (uint8_t)(16U + lineNumber * 16U),
		                line,
		                OLED_8X16);

		if (message[sourceIndex] == '\0')
		{
			break;
		}
	}

	OLED_Update();
}

int main(void)
{
	uint8_t temperature = 0U;
	uint8_t humidity = 0U;
	uint8_t dhtError;
	uint8_t temperatureAlarm;
	uint8_t humidityAlarm;
	uint8_t gasAlarm;
	uint8_t wifiTaskStatus;
	uint16_t gasPpm;
	WifiDisplayState wifiDisplayState;
	char wifiMessage[WIFI_MESSAGE_SIZE];

	SystemCoreClockUpdate();
	delay_init((uint8_t)(SystemCoreClock / 1000000U));
	wifiMessage[0] = '\0';
	OLED_Init();
	wifiDisplayState = WIFI_DISPLAY_LINKING;
	OLED_ShowMessage(wifiMessage, wifiDisplayState);
	MY_ADC_Init();
	LED_Init();
	BEEP_Init();
	dhtError = DHT11_Init();
	delay_ms(1000);
	(void)ESP8266_Init();
	wifiDisplayState = (ESP8266_IsWifiConnected() != 0U) ?
	                   WIFI_DISPLAY_LINKED : WIFI_DISPLAY_FAILED;
	OLED_ShowMessage(wifiMessage, wifiDisplayState);

	while(1)
	{
		dhtError = DHT11_Read_Data(&temperature, &humidity);
		gasPpm = (uint16_t)(MQ135_GetData() + 0.5f);

		temperatureAlarm = (dhtError == 0U &&
		                    temperature >= TEMP_HIGH_THRESHOLD_C) ? 1U : 0U;
		humidityAlarm = (dhtError == 0U &&
		                 humidity >= HUMIDITY_HIGH_THRESHOLD_RH) ? 1U : 0U;
		gasAlarm = (gasPpm > GAS_HIGH_THRESHOLD_PPM) ? 1U : 0U;

		if ((temperatureAlarm != 0U) ||
		    (humidityAlarm != 0U) ||
		    (gasAlarm != 0U))
		{
			LED_On();
			BEEP_On();
		}
		else
		{
			LED_Off();
			BEEP_Off();
		}

		ESP8266_SetData(gasPpm, temperature, humidity);
		wifiTaskStatus = ESP8266_Task();
		if (wifiTaskStatus == ESP8266_STATUS_RECONNECT_REQUIRED)
		{
			wifiDisplayState = WIFI_DISPLAY_LINKING;
			OLED_ShowMessage(wifiMessage, wifiDisplayState);
			(void)ESP8266_Init();
		}
		(void)ESP8266_GetMessage(wifiMessage, sizeof(wifiMessage));
		wifiDisplayState = (ESP8266_IsWifiConnected() != 0U) ?
		                   WIFI_DISPLAY_LINKED : WIFI_DISPLAY_FAILED;
		OLED_ShowMessage(wifiMessage, wifiDisplayState);

		/* DHT11 采样间隔不小于 1 s。 */
		delay_ms(1000);
	}
}
