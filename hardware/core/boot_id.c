#include "boot_id.h"

static void write_hex(uint32_t value, char *output)
{
    static const char hex[] = "0123456789abcdef";
    uint8_t index;

    for (index = 0U; index < 8U; index++)
    {
        output[index] = hex[(value >> ((7U - index) * 4U)) & 0x0FU];
    }
}

void BootIdFormat(uint32_t identity, uint32_t entropy,
                  char output[BOOT_ID_LENGTH + 1U])
{
    write_hex(identity, output);
    write_hex(identity ^ entropy, output + 8U);
    output[BOOT_ID_LENGTH] = '\0';
}
