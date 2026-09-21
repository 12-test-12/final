#ifndef __ESP8266_H
#define __ESP8266_H

#include "stm32f10x.h"
#include <stdint.h>
#include "delay.h"

/*
 * Deployment values belong in esp8266_config.local.h, which is ignored by
 * Git. The checked-in defaults keep clean builds possible but are not valid
 * network credentials.
 */
#if defined(__has_include)
#if __has_include("esp8266_config.local.h")
#include "esp8266_config.local.h"
#endif
#endif

#ifndef WIFI_SSID
#define WIFI_SSID       "CHANGE_ME"
#endif
#ifndef WIFI_PASSWORD
#define WIFI_PASSWORD   "CHANGE_ME"
#endif
#ifndef SERVER_IP
#define SERVER_IP       "127.0.0.1"
#endif
#ifndef SERVER_PORT
#define SERVER_PORT     "8081"
#endif
#ifndef DEVICE_ID
#define DEVICE_ID       "MCU001"
#endif

#define ESP8266_USART       USART1
#define ESP8266_GPIO_PORT   GPIOA
#define ESP8266_TX_PIN      GPIO_Pin_9
#define ESP8266_RX_PIN      GPIO_Pin_10
#define ESP8266_BAUD_RATE   115200U

typedef enum
{
    ESP8266_STATUS_OK = 0U,
    ESP8266_STATUS_AT_ERROR,
    ESP8266_STATUS_WIFI_ERROR,
    ESP8266_STATUS_TCP_ERROR,
    ESP8266_STATUS_SEND_ERROR,
    ESP8266_STATUS_RECONNECT_REQUIRED,
    ESP8266_STATUS_JOIN_TIMEOUT,
    ESP8266_STATUS_WRONG_PASSWORD,
    ESP8266_STATUS_AP_NOT_FOUND,
    ESP8266_STATUS_JOIN_FAILED
} ESP8266_Status;

uint8_t ESP8266_Init(void);
uint8_t ESP8266_Task(void);
uint8_t ESP8266_IsWifiConnected(void);
uint8_t ESP8266_IsTcpConnected(void);
uint8_t ESP8266_OpenTcp(void);
uint8_t ESP8266_SendBytes(const uint8_t *payload, uint16_t length);
uint8_t ESP8266_GetPacket(uint8_t *buffer, uint16_t capacity, uint16_t *length);
void ESP8266_SetData(uint16_t gasPpm, uint8_t temperature, uint8_t humidity);
char ESP8266_GetCmd(void);
uint8_t ESP8266_GetMessage(char *buffer, uint16_t capacity);

#endif
