#ifndef __ADC_H
#define __ADC_H

#include "stm32f10x.h"

#define ADC_PORT        GPIOA
#define MQ135_PIN       GPIO_Pin_1

void MY_ADC_Init(void);
uint16_t MY_ADC_GetValue(void);
uint16_t ADC_GetAvgValue(void);
float MQ135_GetData(void);

/* 保留旧名称兼容已有调用。 */
#define MQ137_GetData MQ135_GetData

#endif

