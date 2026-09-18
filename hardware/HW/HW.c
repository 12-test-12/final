#include "HW.h"


void HW_Init(void)
{
	RCC_APB2PeriphClockCmd(HW_RCC,ENABLE);
	GPIO_InitTypeDef GPIO_InitStruct;
	GPIO_InitStruct.GPIO_Mode = GPIO_Mode_IN_FLOATING;
	GPIO_InitStruct.GPIO_Pin = HW_PIN;
	GPIO_InitStruct.GPIO_Speed = GPIO_Speed_50MHz;
	GPIO_Init(HW_PORT, &GPIO_InitStruct);
}

uint8_t HW_Getstate(void)
{
	return GPIO_ReadInputDataBit(HW_PORT,HW_PIN);
}

