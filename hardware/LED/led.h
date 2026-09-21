#ifndef __LED_H
#define __LED_H

#include "stm32f10x.h" // Device header

#define LED_GPIO_PROT GPIOA
#define LED_GPIO_PIN GPIO_Pin_4

/* Buzzer signal: TIM1 channel 1 main output. PA13 remains reserved for SWDIO. */
#define BEEP_GPIO_PORT GPIOA
#define BEEP_GPIO_PIN GPIO_Pin_8

void LED_Init(void);
void LED_Toggle(void);
void LED_On(void);
void LED_Off(void);
void LED_Twinkle(void);
void BEEP_Init(void);
void BEEP_Off(void);
void BEEP_On(void);
#endif
