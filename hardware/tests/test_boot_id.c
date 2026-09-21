#include "boot_id.h"
#include "test_support.h"

#include <string.h>

void test_boot_id_suite(void)
{
    char output[BOOT_ID_LENGTH + 1U];
    uint32_t index;

    BootIdFormat(0x0123ABCDU, 0x55AA00FFU, output);
    CHECK_INT(BOOT_ID_LENGTH, strlen(output));
    CHECK_TRUE(strcmp(output, "0123abcd5489ab32") == 0);
    for (index = 0U; index < BOOT_ID_LENGTH; index++)
    {
        CHECK_TRUE((output[index] >= '0' && output[index] <= '9') ||
                   (output[index] >= 'a' && output[index] <= 'f'));
    }
}
