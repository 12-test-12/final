#ifndef __FLASH_CONFIG_H
#define __FLASH_CONFIG_H

/*
 * The platform side of the threshold store: three operations over the reserved
 * Flash pages.
 *
 * It is deliberately thin. Everything that decides *what* to write, in which
 * order, and whether the result is trustworthy lives in core/threshold_store.c,
 * where it is covered by host tests including the power-loss paths. This file
 * only turns those calls into Standard Peripheral Library calls.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "threshold_store.h"

/* Build the Flash port over the reserved pages.
 *
 * The returned port points at no mutable state, so the caller may keep it for
 * the lifetime of the program. */
const ThresholdFlashPort *FlashConfigPort(void);

/*
 * 阈值区自检。上电时调用一次，用于尽早发现保留页被其他代码写入或擦除的情况。
 *
 * 返回 true 表示两页都处于"有记录"或"已擦除"的可识别状态；返回 false 表示
 * 出现了既不是有效记录也不是全 1 的内容，此时调用方应使用编译期默认阈值并
 * 把该情况上报。
 */
bool FlashConfigSelfCheck(void);

#endif /* __FLASH_CONFIG_H */
