#ifndef __APP_CONFIG_H
#define __APP_CONFIG_H

/*
 * Compile-time device configuration.
 *
 * These values are the single source of the defaults: hardware/core/env_monitor.h
 * derives its own default macros from them, so changing a limit here changes both
 * the firmware behaviour and the version-1 thresholds the backend reports.
 * docs/device-protocol.md defines threshold version 1 as exactly this set.
 */

/* 第 1 组：温度上限，单位：摄氏度 */
#define TEMP_HIGH_THRESHOLD_C       30U

/* 第 2 组：湿度上限，单位：%RH */
#define HUMIDITY_HIGH_THRESHOLD_RH  80U

/* 第 3 组：气体浓度上限，单位：ppm（未标定曲线的估算值，与遥测 gasPpm 同单位） */
#define GAS_HIGH_THRESHOLD_PPM      20U

/*
 * 快速通道阈值。绝对阈值只能在读数已经越界后报警；这两项让设备在读数仍然
 * 正常但变化过快时提前报警。
 *
 * - 温度突增：在 gas/温度滑动窗口（ENV_RAPID_WINDOW_MS，默认 60 秒）内累计上升
 *   的摄氏度数。DHT11 分辨率为 1 °C，因此该值不宜小于 2。
 * - 气体突增：同一窗口内 gasAdcFiltered 的增量，单位是 ADC 码而不是 ppm。
 *   增量是差值，在传感器未标定时仍然有效；ADC 值也始终可用。
 */
#define TEMP_RISE_THRESHOLD_C       3U
#define GAS_RISE_THRESHOLD_ADC      150U

/*
 * 阈值掉电保存区。
 *
 * STM32F103C8T6 没有 EEPROM，因此用保留的两页 Flash 交替存放配置记录：
 * 写新记录时先擦除非当前槽位，写完后读回逐字节校验，校验通过才切换当前槽位。
 * 任何时刻断电，另一个槽位仍保存着上一次可用配置。链接脚本已把代码区缩短到
 * 62 KiB（见 Linker/STM32F103C8Tx_FLASH.ld），因此代码不会长到这两页里。
 *
 * 每次只能通过 SDK 的 FLASH_ErasePage/FLASH_ProgramWord 写入，且写入地址必须
 * 按半字对齐；驱动实现见 User/flash_config.c。
 */
#define THRESHOLD_FLASH_SLOT0_ADDR  0x0800F800U
#define THRESHOLD_FLASH_SLOT1_ADDR  0x0800FC00U

/*
 * 上电自检（bring-up 辅助）。
 *
 * LED 与蜂鸣器平时只在报警时输出，而 DHT11 一旦读取失败就会持续报警，因此
 * 「蜂鸣器不响」既可能是驱动没工作，也可能是报警本来就没触发，现场无法区分。
 * 上电时让 PA4 与 PA8 同时输出一小段时间，可以在完全不依赖传感器读数与阈值
 * 判断的前提下单独验证这条输出链路：能响/能亮说明驱动与接线成立，剩下的问题
 * 就只在报警判定或接线引脚上。
 *
 * 这是调试辅助，不属于报警逻辑：它不读传感器、不参与阈值判断、运行期间不再
 * 触发。现场验收完成后把 HARDWARE_SELFTEST_ON_BOOT 置 0 即可关闭。
 */
#define HARDWARE_SELFTEST_ON_BOOT   0
#define HARDWARE_SELFTEST_MS        300U

/* Buzzer policy for the current bring-up stage. Only gas-related causes are
 * audible; temperature, humidity and sensor faults remain visible on LED/OLED
 * and in telemetry. At the 100 ms main-loop cadence this is 200 ms on and
 * 800 ms off, avoiding a continuously sounding buzzer. */
#define GAS_BUZZER_PERIOD_TICKS     10U
#define GAS_BUZZER_ON_TICKS          2U

#endif
