#ifndef __DHT11_H
#define __DHT11_H

#include "delay.h"
#include "stm32f10x.h" // Device header

/* DHT11 数据引脚。 */
#define DHT11_GPIO_PORT GPIOA
#define DHT11_GPIO_PIN GPIO_Pin_5
#define DHT11_GPIO_CLK RCC_APB2Periph_GPIOA
#define OUT 1
#define IN 0

#define DHT11_Low GPIO_ResetBits(DHT11_GPIO_PORT, DHT11_GPIO_PIN)
#define DHT11_High GPIO_SetBits(DHT11_GPIO_PORT, DHT11_GPIO_PIN)

/*
 * 现场诊断：把读取过程中驱动看到的每一次边沿连同时间戳记录下来，供 SWD 读回。
 *
 * DHT11_Read_Data 的错误码只能说明"哪一步超时"，不能说明线上到底发生了什么。
 * 打开这项后，dht_wait_until 每等到一次边沿就记一笔（level 0/1），每次等待
 * 超时也记一笔（level 2）；一次读取失败则保留轨迹，读取成功则清空，因此
 * DhtTrace* 里始终是**最近一次失败**的完整现场。据此可以区分：
 *   - 只有 0~1 笔：器件完全没有应答 —— 接线、供电或器件本身。
 *   - 83 笔且时间戳规整：整帧都读到了，问题在宽度判定或校验。
 *   - 边沿数少于 83：中途漏掉了位（轮询太慢或被中断打断）。
 *   - 同步点错位：第一笔到第二笔的间隔是 80 us（应答低电平）还是 50 us
 *     （第一个数据位的低电平）—— 后者说明错过了应答，整帧会错开一位。
 *
 * 2026-09-21 的现场排查就是靠它把范围收敛到同步相位上的。排查结束后保持
 * 关闭；它只做诊断，不参与正常读取。
 */
#define DHT11_TRACE_ENABLE 0

#if DHT11_TRACE_ENABLE
#define DHT11_TRACE_DEPTH 192U

extern volatile uint32_t DhtTraceCycle[DHT11_TRACE_DEPTH];
extern volatile uint32_t DhtTraceLevel[DHT11_TRACE_DEPTH];
extern volatile uint32_t DhtTraceCount;
#endif

/**
 * @brief  初始化 DHT11 数据引脚，并使能判位用的周期计数器。
 * @note   调用后数据线为带上拉的输入（空闲电平确定），等待后续 DHT11_Rst 拉低。
 * @retval 固定返回 0。
 */
uint8_t DHT11_Init(void);

/**
 * @brief  读取一次温湿度（整数部分）。
 * @param  temp 输出：温度整数部分，单位 ℃。仅当返回 0 时有效。
 * @param  humi 输出：相对湿度整数部分，单位 %RH。仅当返回 0 时有效。
 * @retval 0 成功。
 * @retval 1 释放总线后器件未拉低总线（无应答）：接线、供电或器件问题。
 * @retval 2 应答电平没有在超时内结束。
 * @retval 3 读位时数据线一直是高电平（帧提前结束）。
 * @retval 4 读位时数据线一直没有回到高电平。
 * @retval 5 校验和不匹配：位宽判定错误或器件型号不符。
 * @retval 6 数值超出 DHT11 合理范围（湿度 > 100 %RH 或温度 > 60 ℃）。
 * @retval 7 调用参数为空指针。
 * @note   失败时不会修改 *temp / *humi；调用方应保留上一次有效读数。
 *         后续分析请改用 docs/device-protocol.md 描述的单位与量程。
 */
uint8_t DHT11_Read_Data(uint8_t *temp, uint8_t *humi);

/**
 * @brief  复位时序：拉低总线 20 ms 后释放，并等待器件应答。
 * @note   自带帧同步：返回后数据线处于第一个数据位的起点。
 *         超时上限见 dht11.c 中的 DHT11_SYNC_TIMEOUT_US。
 */
uint8_t DHT11_Check(void);

/**
 * @brief  复位器件并释放总线，不等待应答（同步由 DHT11_Check 完成）。
 */
void DHT11_Rst(void);

/**
 * @brief  切换数据引脚方向。
 * @param  mode OUT 为开漏输出（电平由 DHT11_Low / DHT11_High 决定），IN 为带上拉输入。
 * @note   开漏输出避免主机与 DHT11 同时驱动总线。
 */
void DHT11_Mode(uint8_t mode);

/**
 * @brief  读取一位，忽略错误码。
 * @retval 0 或 1；超时按 0 返回。
 */
uint8_t DHT11_Read_Bit(void);

/**
 * @brief  读取一个字节，忽略错误码。
 * @retval 读到的字节；超时返回 0。
 */
uint8_t DHT11_Read_Byte(void);

#if DHT11_TRACE_ENABLE
/**
 * @brief  清空边沿轨迹缓冲。
 * @note   轨迹由 dht_wait_until 在正常读取过程中记录，本函数只用于手动复位。
 */
void DHT11_TraceReset(void);
#endif

#endif
