#include "threshold_store.h"

/* Record layout, 20 bytes:
 *   0  magic          4  "LTHR" identifies the record and its owner
 *   4  schemaVersion  1  version of this layout
 *   5  version        4  the threshold version this record carries, little-endian
 *   9  temperatureHighC  1
 *  10  humidityHighRh    1
 *  11  gasHighPpm        2  little-endian
 *  13  temperatureRiseC  1
 *  14  gasRiseAdc        2  little-endian
 *  16  reserved         0
 *  16  crc32            4  over bytes 0..15, little-endian
 *
 * The integers are written in a fixed byte order rather than as a struct image,
 * so the record does not change meaning with the compiler's padding or
 * endianness. A field is at the same offset in every build.
 */
#define THRESHOLD_RECORD_SIZE 20U
#define THRESHOLD_RECORD_MAGIC_0 'L'
#define THRESHOLD_RECORD_MAGIC_1 'T'
#define THRESHOLD_RECORD_MAGIC_2 'H'
#define THRESHOLD_RECORD_MAGIC_3 'R'
#define THRESHOLD_RECORD_SCHEMA 1U

/* Offset of each field within the record. */
#define OFFSET_MAGIC 0U
#define OFFSET_SCHEMA 4U
#define OFFSET_VERSION 5U
#define OFFSET_TEMPERATURE_HIGH 9U
#define OFFSET_HUMIDITY_HIGH 10U
#define OFFSET_GAS_HIGH 11U
#define OFFSET_TEMPERATURE_RISE 13U
#define OFFSET_GAS_RISE 14U
#define OFFSET_CRC 16U

/* Bytes covered by the CRC. Everything before it. */
#define CRC_COVERAGE OFFSET_CRC

/* Number of bytes in the record's payload area, excluding the CRC itself. */
#define RECORD_BODY_SIZE OFFSET_CRC

/* Write a little-endian 16-bit value. */
static void put_uint16(uint8_t *target, uint16_t value)
{
    target[0] = (uint8_t)(value & 0xFFU);
    target[1] = (uint8_t)((value >> 8) & 0xFFU);
}

/* Write a little-endian 32-bit value. */
static void put_uint32(uint8_t *target, uint32_t value)
{
    target[0] = (uint8_t)(value & 0xFFU);
    target[1] = (uint8_t)((value >> 8) & 0xFFU);
    target[2] = (uint8_t)((value >> 16) & 0xFFU);
    target[3] = (uint8_t)((value >> 24) & 0xFFU);
}

/* Read a little-endian 16-bit value. */
static uint16_t get_uint16(const uint8_t *source)
{
    return (uint16_t)(((uint16_t)source[1] << 8) | (uint16_t)source[0]);
}

/* Read a little-endian 32-bit value. */
static uint32_t get_uint32(const uint8_t *source)
{
    return ((uint32_t)source[3] << 24) | ((uint32_t)source[2] << 16) |
           ((uint32_t)source[1] << 8) | (uint32_t)source[0];
}

uint32_t ThresholdStoreCrc32(const uint8_t *data, uint32_t length)
{
    uint32_t crc = 0xFFFFFFFFU;
    uint32_t index;
    uint8_t bit;

    if (data == NULL)
    {
        return 0U;
    }

    /* Bitwise rather than table-driven: the records are twenty bytes, so the
     * loop runs a few thousand times in total, and a table would cost 1 KiB of
     * the part's 64 KiB Flash to save a fraction of a millisecond once per
     * configuration change. */
    for (index = 0U; index < length; index++)
    {
        crc ^= (uint32_t)data[index];
        for (bit = 0U; bit < 8U; bit++)
        {
            if ((crc & 1U) != 0U)
            {
                crc = (crc >> 1) ^ 0xEDB88320U;
            }
            else
            {
                crc >>= 1;
            }
        }
    }
    return crc ^ 0xFFFFFFFFU;
}

/* Serialise a record into `out`. */
static void record_encode(uint8_t *out, const EnvThresholds *thresholds, uint32_t version)
{
    uint32_t index;

    for (index = 0U; index < THRESHOLD_RECORD_SIZE; index++)
    {
        out[index] = 0U;
    }
    out[OFFSET_MAGIC + 0U] = (uint8_t)THRESHOLD_RECORD_MAGIC_0;
    out[OFFSET_MAGIC + 1U] = (uint8_t)THRESHOLD_RECORD_MAGIC_1;
    out[OFFSET_MAGIC + 2U] = (uint8_t)THRESHOLD_RECORD_MAGIC_2;
    out[OFFSET_MAGIC + 3U] = (uint8_t)THRESHOLD_RECORD_MAGIC_3;
    out[OFFSET_SCHEMA] = (uint8_t)THRESHOLD_RECORD_SCHEMA;
    put_uint32(&out[OFFSET_VERSION], version);
    out[OFFSET_TEMPERATURE_HIGH] = thresholds->temperature_high_c;
    out[OFFSET_HUMIDITY_HIGH] = thresholds->humidity_high_rh;
    put_uint16(&out[OFFSET_GAS_HIGH], thresholds->gas_high_ppm);
    out[OFFSET_TEMPERATURE_RISE] = thresholds->temperature_rise_c;
    put_uint16(&out[OFFSET_GAS_RISE], thresholds->gas_rise_adc);
    put_uint32(&out[OFFSET_CRC], ThresholdStoreCrc32(out, CRC_COVERAGE));
}

/* Parse a record, checking the magic, the schema and the CRC before trusting
 * any field. */
static bool record_decode(const uint8_t *in, ThresholdSlot *slot)
{
    uint32_t stored_crc;
    uint32_t computed_crc;

    if (in[OFFSET_MAGIC + 0U] != (uint8_t)THRESHOLD_RECORD_MAGIC_0 ||
        in[OFFSET_MAGIC + 1U] != (uint8_t)THRESHOLD_RECORD_MAGIC_1 ||
        in[OFFSET_MAGIC + 2U] != (uint8_t)THRESHOLD_RECORD_MAGIC_2 ||
        in[OFFSET_MAGIC + 3U] != (uint8_t)THRESHOLD_RECORD_MAGIC_3)
    {
        /* Erased Flash reads as all ones and a half-written record has no magic
         * yet; both are "no configuration here" rather than a fault. */
        return false;
    }
    if (in[OFFSET_SCHEMA] != (uint8_t)THRESHOLD_RECORD_SCHEMA)
    {
        return false;
    }
    stored_crc = get_uint32(&in[OFFSET_CRC]);
    computed_crc = ThresholdStoreCrc32(in, CRC_COVERAGE);
    if (stored_crc != computed_crc)
    {
        /* A record that fails its CRC is a torn or corrupted write. It is
         * ignored so the other slot, which still holds the previous
         * configuration, becomes current. */
        return false;
    }

    slot->valid = true;
    slot->version = get_uint32(&in[OFFSET_VERSION]);
    slot->thresholds.temperature_high_c = in[OFFSET_TEMPERATURE_HIGH];
    slot->thresholds.humidity_high_rh = in[OFFSET_HUMIDITY_HIGH];
    slot->thresholds.gas_high_ppm = get_uint16(&in[OFFSET_GAS_HIGH]);
    slot->thresholds.temperature_rise_c = in[OFFSET_TEMPERATURE_RISE];
    slot->thresholds.gas_rise_adc = get_uint16(&in[OFFSET_GAS_RISE]);

    if (slot->version < THRESHOLD_RECORD_MIN_VERSION)
    {
        /* A record whose version is below the first real version cannot have
         * come from a configuration, so it is treated as absent. */
        slot->valid = false;
        return false;
    }
    return true;
}

/* Read every slot from Flash, marking the ones that do not hold a valid record. */
void ThresholdStoreReload(ThresholdStore *store)
{
    uint32_t index;

    store->current = -1;
    for (index = 0U; index < THRESHOLD_RECORD_SLOTS; index++)
    {
        uint8_t raw[THRESHOLD_RECORD_SIZE];

        store->slots[index].valid = false;
        if (store->port == NULL || store->port->read == NULL)
        {
            continue;
        }
        if (!store->port->read(store->port->context, store->port->slot_offset[index],
                               raw, sizeof(raw)))
        {
            continue;
        }
        if (record_decode(raw, &store->slots[index]) &&
            (store->current < 0 || store->slots[index].version > store->slots[store->current].version))
        {
            store->current = (int)index;
        }
    }
}

void ThresholdStoreInit(ThresholdStore *store, const ThresholdFlashPort *port)
{
    if (store == NULL)
    {
        return;
    }
    store->port = port;
    ThresholdStoreReload(store);
}

void ThresholdStoreMarkEmpty(ThresholdStore *store)
{
    uint32_t index;

    if (store == NULL)
    {
        return;
    }
    for (index = 0U; index < THRESHOLD_RECORD_SLOTS; index++)
    {
        store->slots[index].valid = false;
        store->slots[index].version = 0U;
    }
    store->current = -1;
}

bool ThresholdStoreLoad(const ThresholdStore *store, EnvThresholds *thresholds, uint32_t *version)
{
    if (store == NULL || store->current < 0)
    {
        return false;
    }
    if (thresholds != NULL)
    {
        *thresholds = store->slots[store->current].thresholds;
    }
    if (version != NULL)
    {
        *version = store->slots[store->current].version;
    }
    return true;
}

ThresholdStoreResult ThresholdStoreSave(ThresholdStore *store, const EnvThresholds *thresholds, uint32_t version)
{
    uint8_t record[THRESHOLD_RECORD_SIZE];
    uint8_t readback[THRESHOLD_RECORD_SIZE];
    ThresholdSlot decoded;
    uint32_t target;
    uint32_t index;

    if (store == NULL || thresholds == NULL || store->port == NULL || store->port->write == NULL ||
        store->port->erase == NULL || store->port->read == NULL)
    {
        return THRESHOLD_STORE_WRITE_FAILED;
    }
    if (store->current >= 0 && version <= store->slots[store->current].version)
    {
        /* Writing an older version would silently undo a newer configuration,
         * which is what a replayed command would ask for. */
        return THRESHOLD_STORE_STALE_VERSION;
    }

    /* The target is the slot that is not current. With one valid slot that is
     * the other one; with none it is the first. Either way the current record is
     * left untouched until the new one has been verified. */
    target = (store->current < 0) ? 0U : (uint32_t)((store->current + 1) % (int)THRESHOLD_RECORD_SLOTS);

    record_encode(record, thresholds, version);

    if (!store->port->erase(store->port->context, store->port->slot_offset[target]))
    {
        /* The current slot is still intact, so the device keeps the
         * configuration it had. */
        return THRESHOLD_STORE_ERASE_FAILED;
    }
    if (!store->port->write(store->port->context, store->port->slot_offset[target],
                            record, sizeof(record)))
    {
        ThresholdStoreReload(store);
        return THRESHOLD_STORE_WRITE_FAILED;
    }
    if (!store->port->read(store->port->context, store->port->slot_offset[target],
                           readback, sizeof(readback)))
    {
        ThresholdStoreReload(store);
        return THRESHOLD_STORE_VERIFY_FAILED;
    }
    /* A byte-for-byte comparison, not only a CRC check: a write that did not
     * take effect but left the previous content in place could still pass a CRC
     * if the previous content happened to be a valid record of another version. */
    for (index = 0U; index < sizeof(record); index++)
    {
        if (readback[index] != record[index])
        {
            ThresholdStoreReload(store);
            return THRESHOLD_STORE_VERIFY_FAILED;
        }
    }
    if (!record_decode(readback, &decoded) || decoded.version != version)
    {
        ThresholdStoreReload(store);
        return THRESHOLD_STORE_VERIFY_FAILED;
    }

    store->slots[target] = decoded;
    store->current = (int)target;
    return THRESHOLD_STORE_OK;
}
