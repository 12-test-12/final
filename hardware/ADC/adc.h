#ifndef __ADC_H
#define __ADC_H

#include "stm32f10x.h"

#define ADC_PORT        GPIOA
#define MQ135_PIN       GPIO_Pin_1

void MY_ADC_Init(void);
uint16_t MY_ADC_GetValue(void);
uint16_t ADC_GetAvgValue(void);
float MQ135_GetData(void);

/**
 * @brief  由给定的 ADC 值估算 NH3 浓度。
 * @param  adc_value ADC 采样值，有效范围 0~4095；超出范围的值会被当作边界值处理。
 * @return 估算浓度，单位 ppm，已限幅到 1.0~999.0。
 * @note   与 MQ135_GetData 使用完全相同的曲线与常数，只是把采样与换算分开。
 *         主循环对**滤波后**的 ADC 值调用本函数，这样上报的 gasPpm 与
 *         gasAdcFiltered 描述的是同一个采样，而不是不同时刻的两个值。
 * @note   该值来自未标定曲线（RL=1kΩ、Ro=10kΩ 为示例值）。在完成预热、
 *         负载电阻确认与标准气体标定之前，它不是定量测量结果，遥测中必须以
 *         gasCalibrated=false 上报。
 */
float MQ135_EstimatePpm(uint16_t adc_value);

/* 保留旧名称兼容已有调用。 */
#define MQ137_GetData MQ135_GetData

#endif

