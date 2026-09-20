#ifndef __THRESHOLD_STORE_H
#define __THRESHOLD_STORE_H

/*
 * Durable storage for the alarm thresholds.
 *
 * The STM32F103C8T6 has no EEPROM, so the configuration lives in a reserved
 * Flash page and is written as two alternating records. The reason for two is
 * power loss: erasing a page takes milliseconds and the erase state reads as
 * all ones, so a single-slot design has a window in which the device has no
 * configuration at all. With two slots the previous record survives untouched
 * while the new one is written, and it is only superseded after the new one has
 * been read back and verified.
 *
 * The store performs no I/O of its own: every access goes through
 * ThresholdFlashPort, which the firmware implements over the Standard Peripheral
 * Library and the host tests implement over an array. That is what makes the
 * power-fail behaviour testable rather than a matter of inspection.
 *
 * This module does not decide *whether* an update is allowed. Range and
 * ordering checks belong to EnvMonitorSetThresholds and the command layer; the
 * store's job is to make whatever it is given survive a reset.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "env_monitor.h"

/* Bytes in one record slot. The record itself is 20 bytes; the slot is a whole
 * Flash page on this part, and erasing is per page, so the slot size is the
 * erase granularity rather than the record length. */
#define THRESHOLD_RECORD_SLOT_SIZE 128U

/* Number of record slots. Two, for the reason in the header comment. */
#define THRESHOLD_RECORD_SLOTS 2U

/* A plausible record with a valid CRC is still refused if its version is below
 * this, so a blank or zeroed page cannot be mistaken for a configuration. */
#define THRESHOLD_RECORD_MIN_VERSION 1U

/* Outcome of a store operation. */
typedef enum
{
    THRESHOLD_STORE_OK = 0,
    /* The port refused an erase. */
    THRESHOLD_STORE_ERASE_FAILED,
    /* The port refused a write. */
    THRESHOLD_STORE_WRITE_FAILED,
    /* The record did not read back as it was written. */
    THRESHOLD_STORE_VERIFY_FAILED,
    /* The port refused to read, or the slot is outside the configured range. */
    THRESHOLD_STORE_READ_FAILED,
    /* The version offered is not newer than the one already stored. */
    THRESHOLD_STORE_STALE_VERSION
} ThresholdStoreResult;

/* The Flash operations the store needs.
 *
 * Each returns true on success. `offset` is relative to the start of the
 * reserved area, not absolute, so the firmware decides where the area lives
 * without the store having to know the memory map. */
typedef struct
{
    bool (*read)(void *context, uint32_t offset, uint8_t *out, uint32_t length);
    /* Erase the page containing `offset`. */
    bool (*erase)(void *context, uint32_t offset);
    bool (*write)(void *context, uint32_t offset, const uint8_t *data, uint32_t length);
    /* Offset of each slot within the reserved area. Both must be page-aligned
     * and at least THRESHOLD_RECORD_SLOT_SIZE apart. */
    uint32_t slot_offset[THRESHOLD_RECORD_SLOTS];
    void *context;
} ThresholdFlashPort;

/* One decoded slot. */
typedef struct
{
    bool valid;
    uint32_t version;
    EnvThresholds thresholds;
} ThresholdSlot;

/* Store state. It holds no pointers to Flash, so it can be a static object. */
typedef struct
{
    const ThresholdFlashPort *port;
    ThresholdSlot slots[THRESHOLD_RECORD_SLOTS];
    /* Index of the slot holding the newest valid record, or -1 for none. */
    int current;
} ThresholdStore;

/* Compute the CRC-32 used by the record format. It is the reflected IEEE 802.3
 * polynomial, so an external tool can verify a dump with a standard crc32. */
uint32_t ThresholdStoreCrc32(const uint8_t *data, uint32_t length);

/* Bind the store to a port and read both slots.
 *
 * After this call, ThresholdStoreLoad reports the newest valid record. A port
 * that fails to read leaves the corresponding slot invalid, which is a safe
 * outcome: the caller falls back to the compile-time defaults. */
void ThresholdStoreInit(ThresholdStore *store, const ThresholdFlashPort *port);

/* Read both slots from Flash again, discarding the cached state. */
void ThresholdStoreReload(ThresholdStore *store);

/* Report the newest valid record.
 *
 * Returns true and fills the outputs when one exists; false means no valid
 * record is stored, and the caller must use the compile-time defaults rather
 * than an all-zero configuration. */
bool ThresholdStoreLoad(const ThresholdStore *store, EnvThresholds *thresholds, uint32_t *version);

/* Write a new record and make it current only after it has been read back and
 * verified.
 *
 * The write goes to the slot that does not hold the current record, so the
 * previous configuration stays intact until the new one is proven. A power loss
 * at any point therefore leaves the device with either the old configuration or
 * the new one, never with none.
 *
 * A version that is not newer than the stored one is refused: a replay of an old
 * command must not undo a newer configuration. */
ThresholdStoreResult ThresholdStoreSave(ThresholdStore *store, const EnvThresholds *thresholds, uint32_t version);

/* Assemble a blank store state for tests and for the first boot, where every
 * slot reads as erased. */
void ThresholdStoreMarkEmpty(ThresholdStore *store);

#endif /* __THRESHOLD_STORE_H */
