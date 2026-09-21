#include "led.h"
#include "delay.h"

void LED_Init(void) {
  // 开启GPIOA时钟
  RCC_APB2PeriphClockCmd(RCC_APB2Periph_GPIOA, ENABLE);

  // 配置LED引脚为推挽输出模式
  GPIO_InitTypeDef GPIO_InitStructure;
  GPIO_InitStructure.GPIO_Mode = GPIO_Mode_Out_PP;
  GPIO_InitStructure.GPIO_Pin = LED_GPIO_PIN;
  GPIO_InitStructure.GPIO_Speed = GPIO_Speed_50MHz;
  GPIO_Init(LED_GPIO_PROT, &GPIO_InitStructure);
  GPIO_ResetBits(LED_GPIO_PROT, LED_GPIO_PIN);
}

void LED_Toggle(void) {
  GPIO_WriteBit(
      LED_GPIO_PROT, LED_GPIO_PIN,
      (BitAction)((1 - GPIO_ReadOutputDataBit(LED_GPIO_PROT,
                                              LED_GPIO_PIN)))); // led电平翻转
}
void LED_On() { GPIO_SetBits(LED_GPIO_PROT, LED_GPIO_PIN); }
void LED_Off() { GPIO_ResetBits(LED_GPIO_PROT, LED_GPIO_PIN); }

void LED_Twinkle() {
  LED_On();
  delay_ms(10);
  LED_Off();
}

void BEEP_Init(void) {
  GPIO_InitTypeDef gpio;
  TIM_TimeBaseInitTypeDef timer;
  TIM_OCInitTypeDef output;
  RCC_ClocksTypeDef clocks;
  uint32_t timerClockHz;
  uint32_t prescaler;
  uint32_t period;

  /* PA8 is TIM1_CH1. A 2 kHz square wave drives a passive buzzer while also
   * remaining audible on the common active-buzzer module. A static high level,
   * used by the former implementation, cannot excite a passive buzzer.
   *
   * The prescaler is derived from the clock the timer actually sees rather
   * than hard-coded for 72 MHz. This board runs from the internal HSI at
   * 8 MHz with no PLL enabled (see Start/system_stm32f10x.c), so TIM1 is
   * clocked at 8 MHz while APB2 is not divided; a prescaler fixed at 72 would
   * have produced about a 222 Hz tone. The counter is held at or below 1 MHz so the
   * tone stays exact for both the 8 MHz HSI and the 72 MHz PLL setup. */
  RCC_APB2PeriphClockCmd(RCC_APB2Periph_GPIOA | RCC_APB2Periph_TIM1, ENABLE);

  RCC_GetClocksFreq(&clocks);
  timerClockHz = clocks.PCLK2_Frequency;
  /* STM32F1 timers run at PCLK when the APB prescaler is 1, and at twice PCLK
   * when the APB prescaler divides the bus. */
  if ((RCC->CFGR & (uint32_t)RCC_CFGR_PPRE2) != 0U) {
    timerClockHz *= 2U;
  }
  prescaler = (timerClockHz + 999999U) / 1000000U; /* counter at most 1 MHz */
  if (prescaler == 0U) {
    prescaler = 1U;
  }
  period = (timerClockHz / prescaler) / 2000U; /* 2 kHz */
  if (period == 0U) {
    period = 1U;
  }

  gpio.GPIO_Mode = GPIO_Mode_AF_PP;
  gpio.GPIO_Pin = BEEP_GPIO_PIN;
  gpio.GPIO_Speed = GPIO_Speed_50MHz;
  GPIO_Init(BEEP_GPIO_PORT, &gpio);

  TIM_TimeBaseStructInit(&timer);
  timer.TIM_Prescaler = (uint16_t)(prescaler - 1U);
  timer.TIM_Period = (uint16_t)(period - 1U);
  timer.TIM_CounterMode = TIM_CounterMode_Up;
  TIM_TimeBaseInit(TIM1, &timer);

  TIM_OCStructInit(&output);
  output.TIM_OCMode = TIM_OCMode_PWM1;
  output.TIM_OutputState = TIM_OutputState_Disable;
  output.TIM_OutputNState = TIM_OutputNState_Disable;
  /* Half the period, so the tone is a square wave and neither the duty cycle
   * nor the drive strength depends on the clock source. */
  output.TIM_Pulse = (uint16_t)(period / 2U);
  output.TIM_OCPolarity = TIM_OCPolarity_High;
  output.TIM_OCIdleState = TIM_OCIdleState_Reset;
  TIM_OC1Init(TIM1, &output);
  TIM_OC1PreloadConfig(TIM1, TIM_OCPreload_Enable);
  TIM_ARRPreloadConfig(TIM1, ENABLE);
  TIM_CtrlPWMOutputs(TIM1, ENABLE);
  TIM_Cmd(TIM1, ENABLE);
}

// 蜂鸣器响
void BEEP_On(void) { TIM_CCxCmd(TIM1, TIM_Channel_1, TIM_CCx_Enable); }

// 蜂鸣器停
void BEEP_Off(void) { TIM_CCxCmd(TIM1, TIM_Channel_1, TIM_CCx_Disable); }
