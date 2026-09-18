#include "adc.h"
#include "delay.h"

/**
 * @brief  获取ADC单次转换采样值（单通道，无DMA，单次转换模式）
 * @note   通道：ADC1 Channel1，对应PA1引脚，MQ135模拟输出
 * @retval ADC原始采样值 0~4095
 */
uint16_t MY_ADC_GetValue(void)
{
    // 配置ADC1规则组通道1，序列第1位，采样时间239.5个ADC时钟周期
    ADC_RegularChannelConfig(ADC1, ADC_Channel_1, 1, ADC_SampleTime_239Cycles5);
    
    // 软件触发，启动一次ADC转换
    ADC_SoftwareStartConvCmd(ADC1, ENABLE);
    
    // 等待转换完成：EOC标志置1表示转换结束
    while(!ADC_GetFlagStatus(ADC1, ADC_FLAG_EOC));
    
    // 读取ADC转换结果寄存器的值并返回
    return ADC_GetConversionValue(ADC1);
}

/**
 * @brief  ADC1初始化函数，独立模式、单次转换，不开启连续转换
 * @note   PA1配置为模拟输入，用于MQ135传感器
 */
void MY_ADC_Init(void)
{
    GPIO_InitTypeDef GPIO_InitStructure;
    ADC_InitTypeDef ADC_InitStructure;
    
    // 使能GPIOA和ADC1外设时钟（APB2域）
    RCC_APB2PeriphClockCmd(RCC_APB2Periph_GPIOA | RCC_APB2Periph_ADC1, ENABLE);
    // ADC时钟分频：PCLK2 /6，ADC时钟最大不能超过14MHz
    RCC_ADCCLKConfig(RCC_PCLK2_Div6);
    
    // PA1 配置为模拟输入模式，接MQ135模拟输出
    GPIO_InitStructure.GPIO_Pin = MQ135_PIN;
    GPIO_InitStructure.GPIO_Mode = GPIO_Mode_AIN;
    GPIO_Init(GPIOA, &GPIO_InitStructure);
    
    // ADC基础参数配置
    ADC_InitStructure.ADC_Mode = ADC_Mode_Independent;        // ADC独立工作模式，不使用多ADC联动
    ADC_InitStructure.ADC_ScanConvMode = DISABLE;              // 关闭扫描模式，单通道不需要扫描
    ADC_InitStructure.ADC_ContinuousConvMode = DISABLE;        // 关闭连续转换，每次需要软件手动触发（重点）
    ADC_InitStructure.ADC_ExternalTrigConv = ADC_ExternalTrigConv_None; // 无外部触发，使用软件触发
    ADC_InitStructure.ADC_DataAlign = ADC_DataAlign_Right;     // 数据右对齐
    ADC_InitStructure.ADC_NbrOfChannel = 1;                    // 规则序列通道数量1个
    ADC_Init(ADC1, &ADC_InitStructure);
    
    // 使能ADC1外设
    ADC_Cmd(ADC1, ENABLE);
    
    // ADC校准流程，提高采样精度
    ADC_ResetCalibration(ADC1);                                // 复位校准寄存器
    while(ADC_GetResetCalibrationStatus(ADC1));                // 等待复位校准完成
    ADC_StartCalibration(ADC1);                                // 启动ADC自校准
    while(ADC_GetCalibrationStatus(ADC1));                     // 等待校准结束
}

/**
 * @brief  ADC多次采样求平均值，降低随机噪声
 * @retval 10次采样平均值，0~4095
 */
uint16_t ADC_GetAvgValue(void)
{
    u32 temp_val = 0;  // 累加和，使用u32防止溢出
    u8 t;
    for(t=0;t<10;t++)  // 循环采集10次
    {
        temp_val += MY_ADC_GetValue();
        delay_ms(5);   // 每次采样间隔5ms，减少传感器波动影响
    }
    return temp_val/10; // 返回平均值
}

/**
 * @brief MQ135传感器读取氨气NH3浓度
 * @note  硬件：负载电阻RL=1kΩ；Ro：洁净空气中传感器电阻=10kΩ
 * @retval NH3浓度估算值，单位ppm，限幅1~999ppm
 */
float MQ135_GetData(void)
{
    float adc_val, voltage, rs, ro;
    adc_val = ADC_GetAvgValue();                              // 获取ADC平均采样值
    voltage = (adc_val / 4095.0f) * 3.3f;                      // ADC原始值转为引脚电压(0~3.3V)

    /* 防止输入为 0V 时在后续计算中除以 0。 */
    if(voltage < 0.001f)
    {
        return 1.0f;
    }

    // Rs：传感器当前电阻
    // 公式：Rs = (VCC - Vout) / Vout * RL
    // VCC=3.3V，RL=1.0kΩ
    rs = (3.3f - voltage) / voltage * 1.0f;
    
    ro = 10.0f;                                                // Ro：洁净空气下MQ135基准电阻10kΩ
    float ratio = rs / ro;                                     // 计算Rs/Ro比值，MQ系列核心参数
    float nh3_conc = 0.0f;

    // MQ135曲线的反平方近似：气体浓度越高，Rs/Ro越小
    // 不使用powf，避免裸机工程引入libm和errno依赖
    if(ratio > 0.001f)
    {
        nh3_conc = 116.30f / (ratio * ratio);
    }
    else
    {
        nh3_conc = 999.0f;
    }

    // 结果限幅，防止异常ADC值导致浓度溢出
    if(nh3_conc < 1.0f) nh3_conc = 1.0f;                       // 最低限制1ppm，避免出现0
    if(nh3_conc > 999.0f) nh3_conc = 999.0f;                   // OLED使用3位显示
    
    return nh3_conc;
}


