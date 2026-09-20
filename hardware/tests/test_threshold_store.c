#include "test_support.h"
#include "threshold_store.h"

/* RAM-backed Flash. It models the three properties the store depends on:
 * erasing a page leaves all ones, a write can fail, and a write can be cut short
 * by a power loss. */
typedef struct
{
    uint8_t memory[THRESHOLD_RECORD_SLOT_SIZE * THRESHOLD_RECORD_SLOTS];
    bool fail_read;
    bool fail_erase;
    bool fail_write;
    /* Write only this many bytes, to model power loss part-way through a write.
     * Zero means write them all. */
    uint32_t write_limit;
    /* Number of erase and write calls, so a test can tell which slot was used. */
    uint32_t erase_calls;
    uint32_t write_calls;
    uint32_t last_erase_offset;
    uint32_t last_write_offset;
} RamFlash;

/* Reset the fixture to a freshly erased part. The counters are cleared too, so
 * a test can assert on them without depending on what ran before. */
static void ram_flash_erase_all(RamFlash *flash)
{
    uint32_t index;

    flash->fail_read = false;
    flash->fail_erase = false;
    flash->fail_write = false;
    flash->write_limit = 0U;
    flash->erase_calls = 0U;
    flash->write_calls = 0U;
    flash->last_erase_offset = 0U;
    flash->last_write_offset = 0U;
    for (index = 0U; index < sizeof(flash->memory); index++)
    {
        flash->memory[index] = 0xFFU;
    }
}

static bool ram_read(void *context, uint32_t offset, uint8_t *out, uint32_t length)
{
    RamFlash *flash = (RamFlash *)context;
    uint32_t index;

    if (flash->fail_read || offset + length > sizeof(flash->memory))
    {
        return false;
    }
    for (index = 0U; index < length; index++)
    {
        out[index] = flash->memory[offset + index];
    }
    return true;
}

static bool ram_erase(void *context, uint32_t offset)
{
    RamFlash *flash = (RamFlash *)context;
    uint32_t index;

    flash->erase_calls++;
    flash->last_erase_offset = offset;
    if (flash->fail_erase || offset + THRESHOLD_RECORD_SLOT_SIZE > sizeof(flash->memory))
    {
        return false;
    }
    for (index = 0U; index < THRESHOLD_RECORD_SLOT_SIZE; index++)
    {
        flash->memory[offset + index] = 0xFFU;
    }
    return true;
}

static bool ram_write(void *context, uint32_t offset, const uint8_t *data, uint32_t length)
{
    RamFlash *flash = (RamFlash *)context;
    uint32_t allowed = length;
    uint32_t index;

    flash->write_calls++;
    flash->last_write_offset = offset;
    if (flash->fail_write || offset + length > sizeof(flash->memory))
    {
        return false;
    }
    if (flash->write_limit > 0U && flash->write_limit < allowed)
    {
        /* The write is cut short and the call never returns, which is exactly
         * what a power loss mid-write looks like to the code that follows. */
        allowed = flash->write_limit;
    }
    for (index = 0U; index < allowed; index++)
    {
        flash->memory[offset + index] = data[index];
    }
    return true;
}

/* Build a port over a RAM flash. */
static void make_port(ThresholdFlashPort *port, RamFlash *flash)
{
    port->read = ram_read;
    port->erase = ram_erase;
    port->write = ram_write;
    port->slot_offset[0] = 0U;
    port->slot_offset[1] = THRESHOLD_RECORD_SLOT_SIZE;
    port->context = flash;
}

/* Build a threshold set for one test case.
 *
 * The result points at a shared scratch object because C cannot take the address
 * of a value returned by a function, and the store takes a pointer. The tests
 * are single-threaded and use the value before the next call. */
static EnvThresholds thresholds_scratch;

static EnvThresholds *thresholds_at(uint8_t temperature, uint8_t humidity, uint16_t gas)
{
    thresholds_scratch.temperature_high_c = temperature;
    thresholds_scratch.humidity_high_rh = humidity;
    thresholds_scratch.gas_high_ppm = gas;
    thresholds_scratch.temperature_rise_c = ENV_DEFAULT_TEMPERATURE_RISE_C;
    thresholds_scratch.gas_rise_adc = ENV_DEFAULT_GAS_RISE_ADC;
    return &thresholds_scratch;
}

/* Exercise the checksum, which is what makes a torn record detectable. */
static void test_crc32(void)
{
    TEST_CASE("the CRC matches the standard check value");
    /* "123456789" is the conventional check input for CRC-32; 0xCBF43926 is the
     * published result. Matching it means an external tool can verify a Flash
     * dump with a stock crc32. */
    {
        const uint8_t input[] = {'1', '2', '3', '4', '5', '6', '7', '8', '9'};

        CHECK_INT(0xCBF43926U, ThresholdStoreCrc32(input, sizeof(input)));
    }

    TEST_CASE("an empty input hashes to zero");
    CHECK_INT(0U, ThresholdStoreCrc32(NULL, 10U));
    CHECK_INT(0U, ThresholdStoreCrc32((const uint8_t *)"", 0U));

    TEST_CASE("a single flipped bit changes the CRC");
    {
        uint8_t a[] = {'a', 'b', 'c', 'd'};
        uint8_t b[] = {'a', 'b', 'c', 'e'};

        CHECK_TRUE(ThresholdStoreCrc32(a, sizeof(a)) != ThresholdStoreCrc32(b, sizeof(b)));
    }
}

/* Exercise the empty and populated cases. */
static void test_round_trip(void)
{
    RamFlash flash;
    ThresholdFlashPort port;
    ThresholdStore store;
    EnvThresholds loaded;
    uint32_t version = 0U;

    TEST_CASE("an erased device reports no stored configuration");
    ram_flash_erase_all(&flash);
    flash.fail_read = false;
    flash.fail_erase = false;
    flash.fail_write = false;
    flash.write_limit = 0U;
    make_port(&port, &flash);
    ThresholdStoreInit(&store, &port);

    /* Returning false is what makes the caller use the compile-time defaults
     * rather than an all-zero threshold set that would alarm immediately. */
    CHECK_FALSE(ThresholdStoreLoad(&store, &loaded, &version));
    CHECK_INT(-1, store.current);

    TEST_CASE("a saved configuration is read back");
    CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&store, thresholds_at(35U, 85U, 120U), 2U));
    CHECK_TRUE(ThresholdStoreLoad(&store, &loaded, &version));
    CHECK_INT(2U, version);
    CHECK_INT(35U, loaded.temperature_high_c);
    CHECK_INT(85U, loaded.humidity_high_rh);
    CHECK_INT(120U, loaded.gas_high_ppm);
    CHECK_INT(ENV_DEFAULT_TEMPERATURE_RISE_C, loaded.temperature_rise_c);
    CHECK_INT(ENV_DEFAULT_GAS_RISE_ADC, loaded.gas_rise_adc);

    TEST_CASE("a reset reads the configuration from Flash, not from RAM");
    {
        ThresholdStore fresh;

        /* Re-initialising over the same memory models the device restarting. */
        ThresholdStoreInit(&fresh, &port);
        CHECK_TRUE(ThresholdStoreLoad(&fresh, &loaded, &version));
        CHECK_INT(2U, version);
        CHECK_INT(120U, loaded.gas_high_ppm);
        CHECK_INT(35U, loaded.temperature_high_c);
    }
}

/* Exercise the alternating slots, which is what makes a power loss survivable. */
static void test_slot_alternation(void)
{
    RamFlash flash;
    ThresholdFlashPort port;
    ThresholdStore store;
    EnvThresholds loaded;
    uint32_t version = 0U;

    ram_flash_erase_all(&flash);
    flash.fail_read = false;
    flash.fail_erase = false;
    flash.fail_write = false;
    flash.write_limit = 0U;
    make_port(&port, &flash);

    TEST_CASE("the first save goes to the first slot");
    ThresholdStoreInit(&store, &port);
    CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&store, thresholds_at(30U, 80U, 80U), 2U));
    CHECK_INT(0U, flash.last_write_offset);
    CHECK_INT(0, store.current);

    TEST_CASE("the second save goes to the other slot");
    /* Writing in place would erase the only copy before the new one exists. */
    CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&store, thresholds_at(31U, 81U, 81U), 3U));
    CHECK_INT(THRESHOLD_RECORD_SLOT_SIZE, flash.last_write_offset);
    CHECK_INT(1, store.current);

    TEST_CASE("the third save returns to the first slot");
    CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&store, thresholds_at(32U, 82U, 82U), 4U));
    CHECK_INT(0U, flash.last_write_offset);
    CHECK_INT(0, store.current);

    TEST_CASE("the newest record wins after a reset");
    {
        ThresholdStore fresh;

        ThresholdStoreInit(&fresh, &port);
        CHECK_TRUE(ThresholdStoreLoad(&fresh, &loaded, &version));
        CHECK_INT(4U, version);
        CHECK_INT(32U, loaded.temperature_high_c);
    }

    TEST_CASE("the newest record wins even when it is in the first slot");
    /* A loader that only looked at the second slot would return the older
     * record here. */
    CHECK_INT(0, store.current);
    CHECK_INT(4U, store.slots[0].version);
    CHECK_INT(3U, store.slots[1].version);
}

/* Exercise the recovery paths, which are the reason for the module. */
static void test_power_loss_recovery(void)
{
    RamFlash flash;
    ThresholdFlashPort port;
    ThresholdStore store;
    EnvThresholds loaded;
    uint32_t version = 0U;

    TEST_CASE("a write cut short by a power loss leaves the previous configuration");
    ram_flash_erase_all(&flash);
    flash.fail_read = false;
    flash.fail_erase = false;
    flash.fail_write = false;
    flash.write_limit = 0U;
    make_port(&port, &flash);
    ThresholdStoreInit(&store, &port);
    CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&store, thresholds_at(30U, 80U, 80U), 2U));

    /* The next save writes only its first bytes. The store reads the record back
     * and compares it byte for byte, so a write that stops part-way is detected
     * and reported rather than being accepted. */
    flash.write_limit = 8U;
    CHECK_INT(THRESHOLD_STORE_VERIFY_FAILED, ThresholdStoreSave(&store, thresholds_at(31U, 81U, 81U), 3U));

    {
        ThresholdStore after_reset;

        ThresholdStoreInit(&after_reset, &port);
        /* The torn record has no valid CRC, so the earlier slot becomes current
         * and the device keeps the configuration it had before the attempt. */
        CHECK_TRUE(ThresholdStoreLoad(&after_reset, &loaded, &version));
        CHECK_INT(2U, version);
        CHECK_INT(80U, loaded.gas_high_ppm);
        CHECK_INT(30U, loaded.temperature_high_c);
    }

    TEST_CASE("an erase that fails leaves the previous configuration");
    {
        RamFlash other;
        ThresholdFlashPort other_port;
        ThresholdStore other_store;
        ThresholdStore after_reset;

        ram_flash_erase_all(&other);
        other.fail_read = false;
        other.fail_erase = false;
        other.fail_write = false;
        other.write_limit = 0U;
        make_port(&other_port, &other);
        ThresholdStoreInit(&other_store, &other_port);
        CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&other_store, thresholds_at(30U, 80U, 80U), 2U));

        other.fail_erase = true;
        CHECK_INT(THRESHOLD_STORE_ERASE_FAILED, ThresholdStoreSave(&other_store, thresholds_at(31U, 81U, 81U), 3U));

        ThresholdStoreInit(&after_reset, &other_port);
        CHECK_TRUE(ThresholdStoreLoad(&after_reset, &loaded, &version));
        CHECK_INT(2U, version);
        CHECK_INT(30U, loaded.temperature_high_c);
    }

    TEST_CASE("a write the port refuses leaves the previous configuration");
    {
        RamFlash other;
        ThresholdFlashPort other_port;
        ThresholdStore other_store;
        ThresholdStore after_reset;

        ram_flash_erase_all(&other);
        other.fail_read = false;
        other.fail_erase = false;
        other.fail_write = false;
        other.write_limit = 0U;
        make_port(&other_port, &other);
        ThresholdStoreInit(&other_store, &other_port);
        CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&other_store, thresholds_at(30U, 80U, 80U), 2U));

        other.fail_write = true;
        CHECK_INT(THRESHOLD_STORE_WRITE_FAILED, ThresholdStoreSave(&other_store, thresholds_at(31U, 81U, 81U), 3U));

        ThresholdStoreInit(&after_reset, &other_port);
        CHECK_TRUE(ThresholdStoreLoad(&after_reset, &loaded, &version));
        CHECK_INT(2U, version);
    }

    TEST_CASE("a single flipped byte invalidates its slot");
    {
        RamFlash other;
        ThresholdFlashPort other_port;
        ThresholdStore other_store;
        ThresholdStore after_reset;

        ram_flash_erase_all(&other);
        other.fail_read = false;
        other.fail_erase = false;
        other.fail_write = false;
        other.write_limit = 0U;
        make_port(&other_port, &other);
        ThresholdStoreInit(&other_store, &other_port);
        CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&other_store, thresholds_at(30U, 80U, 80U), 2U));
        CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&other_store, thresholds_at(31U, 81U, 81U), 3U));

        /* Corrupt the record in the newer slot. */
        other.memory[THRESHOLD_RECORD_SLOT_SIZE + 9U] ^= 0x01U;

        ThresholdStoreInit(&after_reset, &other_port);
        CHECK_TRUE(ThresholdStoreLoad(&after_reset, &loaded, &version));
        /* The older record is still intact and becomes current. */
        CHECK_INT(2U, version);
        CHECK_INT(30U, loaded.temperature_high_c);
    }

    TEST_CASE("a device with both slots corrupt reports no configuration");
    {
        RamFlash other;
        ThresholdFlashPort other_port;
        ThresholdStore other_store;
        ThresholdStore after_reset;

        ram_flash_erase_all(&other);
        other.fail_read = false;
        other.fail_erase = false;
        other.fail_write = false;
        other.write_limit = 0U;
        make_port(&other_port, &other);
        ThresholdStoreInit(&other_store, &other_port);
        CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&other_store, thresholds_at(30U, 80U, 80U), 2U));
        other.memory[9U] ^= 0x01U;

        ThresholdStoreInit(&after_reset, &other_port);
        /* The caller then falls back to the compile-time defaults, which is the
         * documented safe outcome rather than an all-zero configuration. */
        CHECK_FALSE(ThresholdStoreLoad(&after_reset, &loaded, &version));
    }

    TEST_CASE("a read the port refuses is treated as a missing configuration");
    {
        RamFlash other;
        ThresholdFlashPort other_port;
        ThresholdStore other_store;

        ram_flash_erase_all(&other);
        other.fail_read = true;
        other.fail_erase = false;
        other.fail_write = false;
        other.write_limit = 0U;
        make_port(&other_port, &other);
        ThresholdStoreInit(&other_store, &other_port);
        CHECK_FALSE(ThresholdStoreLoad(&other_store, &loaded, &version));
    }
}

/* Exercise the version rules. */
static void test_version_rules(void)
{
    RamFlash flash;
    ThresholdFlashPort port;
    ThresholdStore store;
    EnvThresholds loaded;
    uint32_t version = 0U;

    ram_flash_erase_all(&flash);
    flash.fail_read = false;
    flash.fail_erase = false;
    flash.fail_write = false;
    flash.write_limit = 0U;
    make_port(&port, &flash);
    ThresholdStoreInit(&store, &port);

    TEST_CASE("a version below the first real one is refused on load");
    {
        uint8_t raw[20];

        /* Build a record by saving, then lower its version and fix the CRC so
         * only the version rule can reject it. */
        CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&store, thresholds_at(30U, 80U, 80U), 1U));
        CHECK_TRUE(ram_read(&flash, 0U, raw, sizeof(raw)));
        raw[5] = 0U;
        raw[6] = 0U;
        raw[7] = 0U;
        raw[8] = 0U;
        {
            uint32_t crc = ThresholdStoreCrc32(raw, 16U);
            raw[16] = (uint8_t)(crc & 0xFFU);
            raw[17] = (uint8_t)((crc >> 8) & 0xFFU);
            raw[18] = (uint8_t)((crc >> 16) & 0xFFU);
            raw[19] = (uint8_t)((crc >> 24) & 0xFFU);
        }
        flash.write_limit = 0U;
        CHECK_TRUE(ram_write(&flash, 0U, raw, sizeof(raw)));

        ThresholdStoreReload(&store);
        /* A record whose version is zero cannot have come from a configuration,
         * so it is treated as absent rather than as version zero. */
        CHECK_FALSE(ThresholdStoreLoad(&store, &loaded, &version));
    }

    TEST_CASE("a version that does not move forward is refused on save");
    ram_flash_erase_all(&flash);
    ThresholdStoreInit(&store, &port);
    CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&store, thresholds_at(30U, 80U, 80U), 4U));
    CHECK_INT(THRESHOLD_STORE_STALE_VERSION, ThresholdStoreSave(&store, thresholds_at(31U, 81U, 81U), 4U));
    CHECK_INT(THRESHOLD_STORE_STALE_VERSION, ThresholdStoreSave(&store, thresholds_at(31U, 81U, 81U), 3U));
    /* The refused saves must not have touched Flash. */
    CHECK_INT(1U, flash.write_calls);
    CHECK_TRUE(ThresholdStoreLoad(&store, &loaded, &version));
    CHECK_INT(4U, version);
    CHECK_INT(30U, loaded.temperature_high_c);
}

/* Exercise the argument and state guards. */
static void test_guards(void)
{
    RamFlash flash;
    ThresholdFlashPort port;
    ThresholdStore store;
    EnvThresholds loaded;
    uint32_t version = 0U;

    ram_flash_erase_all(&flash);
    flash.fail_read = false;
    flash.fail_erase = false;
    flash.fail_write = false;
    flash.write_limit = 0U;
    make_port(&port, &flash);

    TEST_CASE("a store with no port reports no configuration");
    ThresholdStoreInit(&store, NULL);
    CHECK_FALSE(ThresholdStoreLoad(&store, &loaded, &version));

    TEST_CASE("a port with no read function reports no configuration");
    {
        ThresholdFlashPort partial = port;

        partial.read = NULL;
        ThresholdStoreInit(&store, &partial);
        CHECK_FALSE(ThresholdStoreLoad(&store, &loaded, &version));
    }

    TEST_CASE("a save over an incomplete port fails rather than claiming success");
    {
        ThresholdFlashPort partial = port;

        partial.write = NULL;
        ThresholdStoreInit(&store, &partial);
        CHECK_INT(THRESHOLD_STORE_WRITE_FAILED, ThresholdStoreSave(&store, thresholds_at(30U, 80U, 80U), 2U));
    }

    TEST_CASE("null arguments are refused");
    ThresholdStoreInit(&store, &port);
    ThresholdStoreMarkEmpty(&store);
    CHECK_FALSE(ThresholdStoreLoad(NULL, &loaded, &version));
    CHECK_FALSE(ThresholdStoreLoad(&store, NULL, NULL));
    CHECK_INT(THRESHOLD_STORE_WRITE_FAILED, ThresholdStoreSave(NULL, thresholds_at(30U, 80U, 80U), 2U));
    CHECK_INT(THRESHOLD_STORE_WRITE_FAILED, ThresholdStoreSave(&store, NULL, 2U));

    TEST_CASE("a load without output pointers still reports presence");
    CHECK_INT(THRESHOLD_STORE_OK, ThresholdStoreSave(&store, thresholds_at(30U, 80U, 80U), 2U));
    CHECK_TRUE(ThresholdStoreLoad(&store, NULL, NULL));

    TEST_CASE("marking a store empty forgets the cached slots");
    ThresholdStoreMarkEmpty(&store);
    CHECK_FALSE(ThresholdStoreLoad(&store, &loaded, &version));
}

/* Entry point for the threshold_store suite. */
void test_threshold_store_suite(void)
{
    test_crc32();
    test_round_trip();
    test_slot_alternation();
    test_power_loss_recovery();
    test_version_rules();
    test_guards();
}
