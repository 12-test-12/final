#include "led.h"
#include "delay.h"

void LED_Init(void)
{
	//开启GPIOB时钟
	RCC_APB2PeriphClockCmd(RCC_APB2Periph_GPIOA, ENABLE);
	
	// 配置LED引脚为推挽输出模式
	GPIO_InitTypeDef GPIO_InitStructure;
	GPIO_InitStructure.GPIO_Mode = GPIO_Mode_Out_PP;
	GPIO_InitStructure.GPIO_Pin = LED_GPIO_PIN;
	GPIO_InitStructure.GPIO_Speed = GPIO_Speed_50MHz;
	GPIO_Init(LED_GPIO_PROT, &GPIO_InitStructure);
	GPIO_ResetBits(LED_GPIO_PROT, LED_GPIO_PIN);
}

void LED_Toggle(void)
{
	GPIO_WriteBit(LED_GPIO_PROT, LED_GPIO_PIN, (BitAction)((1-GPIO_ReadOutputDataBit(LED_GPIO_PROT, LED_GPIO_PIN))));//led电平翻转
}
void LED_On()
{
	GPIO_SetBits(LED_GPIO_PROT, LED_GPIO_PIN);
}
void LED_Off()
{
	GPIO_ResetBits(LED_GPIO_PROT, LED_GPIO_PIN);
}

void LED_Twinkle()
{
	LED_On();
	delay_ms(10);
	LED_Off();
}

void BEEP_Init(void)
{
	GPIO_InitTypeDef gpio;
	TIM_TimeBaseInitTypeDef timer;
	TIM_OCInitTypeDef output;

	/* PB13 is TIM1_CH1N. A 2 kHz square wave drives a passive buzzer while also
	 * remaining audible on the common active-buzzer module. A static high level,
	 * used by the former implementation, cannot excite a passive buzzer. */
	RCC_APB2PeriphClockCmd(RCC_APB2Periph_GPIOB | RCC_APB2Periph_TIM1, ENABLE);
	gpio.GPIO_Mode = GPIO_Mode_AF_PP;
	gpio.GPIO_Pin = GPIO_Pin_13;
	gpio.GPIO_Speed = GPIO_Speed_50MHz;
	GPIO_Init(GPIOB, &gpio);

	TIM_TimeBaseStructInit(&timer);
	timer.TIM_Prescaler = 71U;       /* 72 MHz / 72 = 1 MHz */
	timer.TIM_Period = 499U;         /* 1 MHz / 500 = 2 kHz */
	timer.TIM_CounterMode = TIM_CounterMode_Up;
	TIM_TimeBaseInit(TIM1, &timer);

	TIM_OCStructInit(&output);
	output.TIM_OCMode = TIM_OCMode_PWM1;
	output.TIM_OutputState = TIM_OutputState_Disable;
	output.TIM_OutputNState = TIM_OutputNState_Disable;
	output.TIM_Pulse = 250U;
	output.TIM_OCNPolarity = TIM_OCNPolarity_High;
	output.TIM_OCNIdleState = TIM_OCNIdleState_Reset;
	TIM_OC1Init(TIM1, &output);
	TIM_OC1PreloadConfig(TIM1, TIM_OCPreload_Enable);
	TIM_ARRPreloadConfig(TIM1, ENABLE);
	TIM_CtrlPWMOutputs(TIM1, ENABLE);
	TIM_Cmd(TIM1, ENABLE);
}

// 蜂鸣器响
void BEEP_On(void)
{
    TIM_CCxNCmd(TIM1, TIM_Channel_1, TIM_CCxN_Enable);
}

// 蜂鸣器停
void BEEP_Off(void)
{
    TIM_CCxNCmd(TIM1, TIM_Channel_1, TIM_CCxN_Disable);
}



