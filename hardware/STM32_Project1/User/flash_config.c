#include "flash_config.h"

#include "app_config.h"
#include "stm32f10x.h"

/* Read bytes from the reserved area.
 *
 * `offset` is relative to slot 0, so the store never needs to know the absolute
 * address of the region. */
static bool flash_read(void *context, uint32_t offset, uint8_t *out, uint32_t length)
{
    uint32_t base = THRESHOLD_FLASH_SLOT0_ADDR;
    uint32_t index;

    (void)context;
    if (out == NULL)
    {
        return false;
    }
    for (index = 0U; index < length; index++)
    {
        /* Flash on this part is memory-mapped, so a read is an ordinary load. */
        out[index] = *(volatile const uint8_t *)(base + offset + index);
    }
    return true;
}

/* Erase one page of the reserved area.
 *
 * A page holds one record slot, which is why the store's slot size matches the
 * page size: erasing the target slot is a single operation on this part. */
static bool flash_erase(void *context, uint32_t offset)
{
    uint32_t address = THRESHOLD_FLASH_SLOT0_ADDR + offset;

    (void)context;
    if ((address % 1024U) != 0U)
    {
        /* FLASH_ErasePage erases the whole page containing the address, so an
         * unaligned offset would silently erase a neighbouring slot. */
        return false;
    }

    FLASH_Unlock();
    /*
     * 清除上电/上次操作遗留的错误标志。若这些标志仍置位，FLASH_ErasePage 会
     * 被拒绝，看起来像"擦除失败"，而实际原因可能只是上一次写入留下的标志。
     */
    FLASH_ClearFlag(FLASH_FLAG_EOP | FLASH_FLAG_PGERR | FLASH_FLAG_WRPRTERR);
    {
        FLASH_Status status = FLASH_ErasePage(address);

        FLASH_Lock();
        return status == FLASH_COMPLETE;
    }
}

/* Write bytes into the reserved area.
 *
 * The Standard Peripheral Library programs half-words, so the data is written
 * two bytes at a time, with a trailing byte padded by 0xFF (the erased value).
 * The store always writes a whole record, so the length is even in practice; the
 * padding keeps the function correct if that ever changes. */
static bool flash_write(void *context, uint32_t offset, const uint8_t *data, uint32_t length)
{
    uint32_t address = THRESHOLD_FLASH_SLOT0_ADDR + offset;
    uint32_t index = 0U;
    bool ok = true;

    (void)context;
    if (data == NULL)
    {
        return false;
    }

    FLASH_Unlock();
    FLASH_ClearFlag(FLASH_FLAG_EOP | FLASH_FLAG_PGERR | FLASH_FLAG_WRPRTERR);

    while (index < length && ok)
    {
        uint16_t half = (uint16_t)data[index];

        if ((index + 1U) < length)
        {
            half = (uint16_t)(half | ((uint16_t)data[index + 1U] << 8));
        }
        else
        {
            /* A lone trailing byte: the high byte stays 0xFF, which is what an
             * erased cell reads as, so a later read of the padding is stable. */
            half = (uint16_t)(half | 0xFF00U);
        }

        if (FLASH_ProgramHalfWord(address, half) != FLASH_COMPLETE)
        {
            ok = false;
            break;
        }
        /* Verify each half-word as it is written. ProgramHalfWord is documented
         * to report success only after its own verification, but a read-back here
         * is what the store's contract actually relies on, and it costs one
         * load per two bytes. */
        if (*(volatile const uint16_t *)address != half)
        {
            ok = false;
            break;
        }

        address += 2U;
        index += 2U;
    }

    FLASH_Lock();
    return ok;
}

const ThresholdFlashPort *FlashConfigPort(void)
{
    static ThresholdFlashPort port;
    static bool initialised = false;

    if (!initialised)
    {
        port.read = flash_read;
        port.erase = flash_erase;
        port.write = flash_write;
        port.slot_offset[0] = 0U;
        port.slot_offset[1] = THRESHOLD_FLASH_SLOT1_ADDR - THRESHOLD_FLASH_SLOT0_ADDR;
        port.context = NULL;
        initialised = true;
    }
    return &port;
}

bool FlashConfigSelfCheck(void)
{
    /*
     * 判断一页是否处于可识别状态：要么开头是记录魔数，要么整页都是 0xFF。
     * 既不是记录、也不是全 1，说明该页被别的代码写过，此时不能假定阈值区可用。
     */
    static const uint8_t magic[4] = {'L', 'T', 'H', 'R'};
    uint32_t slot;

    for (slot = 0U; slot < THRESHOLD_RECORD_SLOTS; slot++)
    {
        uint32_t base = THRESHOLD_FLASH_SLOT0_ADDR + (slot * 1024U);
        bool all_erased = true;
        bool magic_ok = true;
        uint32_t index;

        for (index = 0U; index < 4U; index++)
        {
            if (*(volatile const uint8_t *)(base + index) != magic[index])
            {
                magic_ok = false;
            }
        }
        for (index = 0U; index < 1024U; index++)
        {
            if (*(volatile const uint8_t *)(base + index) != 0xFFU)
            {
                all_erased = false;
                break;
            }
        }
        if (!magic_ok && !all_erased)
        {
            return false;
        }
    }
    return true;
}
