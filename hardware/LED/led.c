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
	//开启GPIOB时钟
	RCC_APB2PeriphClockCmd(RCC_APB2Periph_GPIOB, ENABLE);
	
	//配置 PB13 为推挽输出模式（蜂鸣器专用）
	GPIO_InitTypeDef GPIO_InitStructure;
	GPIO_InitStructure.GPIO_Mode = GPIO_Mode_Out_PP;      // 推挽输出
	GPIO_InitStructure.GPIO_Pin = GPIO_Pin_13;
	GPIO_InitStructure.GPIO_Speed = GPIO_Speed_50MHz;
	GPIO_Init(GPIOB, &GPIO_InitStructure);                // 端口：GPIOB
	
	GPIO_ResetBits(GPIOB, GPIO_Pin_13);  // 默认低电平（蜂鸣器不响）
}

// 蜂鸣器响
void BEEP_On(void)
{
    GPIO_SetBits(GPIOB, GPIO_Pin_13);
}

// 蜂鸣器停
void BEEP_Off(void)
{
    GPIO_ResetBits(GPIOB, GPIO_Pin_13);
}



