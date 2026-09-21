#ifndef __BOOT_ID_H
#define __BOOT_ID_H

#include <stdint.h>

#define BOOT_ID_LENGTH 16U

/* Format two 32-bit entropy words as the protocol's fixed-length alphanumeric
 * boot identifier. The caller supplies MCU identity and per-boot entropy. */
void BootIdFormat(uint32_t identity, uint32_t entropy,
                  char output[BOOT_ID_LENGTH + 1U]);

#endif
