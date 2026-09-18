#ifndef __HW_H
#define __HW_H

#include "stm32f10x.h" 

#define HW_RCC 	RCC_APB2Periph_GPIOA
#define HW_PORT GPIOA
#define HW_PIN 	GPIO_Pin_0

void HW_Init(void);
uint8_t HW_Getstate(void);

#endif
