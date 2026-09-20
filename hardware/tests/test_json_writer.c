#include "json_writer.h"
#include "test_support.h"

/* Exercise the writer's formatting and escaping. */
static void test_basic_output(void)
{
    char buffer[64];
    JsonWriter writer;

    TEST_CASE("an empty writer holds an empty string");
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    CHECK_TRUE(JsonWriterOk(&writer));
    CHECK_INT(0, JsonWriterLength(&writer));
    CHECK_INT(0, strcmp(buffer, ""));

    TEST_CASE("object punctuation and keys are written verbatim");
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    JsonWriterRaw(&writer, "{");
    JsonWriterKey(&writer, "a");
    JsonWriterUnsigned(&writer, 42U);
    JsonWriterRaw(&writer, "}");
    CHECK_TRUE(JsonWriterOk(&writer));
    CHECK_INT(0, strcmp(buffer, "{\"a\":42}"));

    TEST_CASE("the length excludes the terminator");
    CHECK_INT((uint32_t)strlen(buffer), JsonWriterLength(&writer));
}

/* Exercise the value kinds. */
static void test_value_kinds(void)
{
    char buffer[128];
    JsonWriter writer;

    TEST_CASE("numbers, booleans and null");
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    JsonWriterUnsigned(&writer, 0U);
    JsonWriterRaw(&writer, ",");
    JsonWriterUnsigned(&writer, 4294967295U);
    JsonWriterRaw(&writer, ",");
    JsonWriterSigned(&writer, -1);
    JsonWriterRaw(&writer, ",");
    JsonWriterScaled1(&writer, 250);
    JsonWriterRaw(&writer, ",");
    JsonWriterBool(&writer, true);
    JsonWriterRaw(&writer, ",");
    JsonWriterBool(&writer, false);
    JsonWriterRaw(&writer, ",");
    JsonWriterNull(&writer);
    CHECK_TRUE(JsonWriterOk(&writer));
    CHECK_INT(0, strcmp(buffer, "0,4294967295,-1,25.0,true,false,null"));

    TEST_CASE("a scaled value keeps one decimal, including a trailing zero");
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    JsonWriterScaled1(&writer, 280);
    CHECK_INT(0, strcmp(buffer, "28.0"));

    TEST_CASE("a scaled negative value keeps its sign");
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    JsonWriterScaled1(&writer, -105);
    CHECK_INT(0, strcmp(buffer, "-10.5"));
}

/* Exercise string escaping, which is what keeps a value from breaking the
 * document. */
static void test_string_escaping(void)
{
    char buffer[128];
    JsonWriter writer;

    TEST_CASE("a plain string is quoted");
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    JsonWriterString(&writer, "MCU001");
    CHECK_INT(0, strcmp(buffer, "\"MCU001\""));

    TEST_CASE("a quote and a backslash are escaped");
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    JsonWriterString(&writer, "a\"b\\c");
    CHECK_INT(0, strcmp(buffer, "\"a\\\"b\\\\c\""));

    TEST_CASE("the short escapes are used for the whitespace controls");
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    JsonWriterString(&writer, "a\nb\rc\td");
    CHECK_INT(0, strcmp(buffer, "\"a\\nb\\rc\\td\""));

    TEST_CASE("any other control character is escaped as a code point");
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    JsonWriterString(&writer, "a\x01z");
    CHECK_INT(0, strcmp(buffer, "\"a\\u0001z\""));

    TEST_CASE("a null pointer writes JSON null, not an empty string");
    /* An empty string would claim the device has a name that is empty; null
     * says there is no value. */
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    JsonWriterString(&writer, NULL);
    CHECK_INT(0, strcmp(buffer, "null"));

    TEST_CASE("the escaped output is well formed JSON");
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    JsonWriterRaw(&writer, "{");
    JsonWriterKey(&writer, "note");
    JsonWriterString(&writer, "say \"hi\"\n");
    JsonWriterRaw(&writer, "}");
    CHECK_TRUE(test_json_is_well_formed(buffer, JsonWriterLength(&writer)));
}

/* Exercise the overflow behaviour, which is what keeps a bounded buffer safe. */
static void test_overflow(void)
{
    char buffer[8];
    JsonWriter writer;

    TEST_CASE("an overflowing write is recorded and nothing is written past the end");
    JsonWriterInit(&writer, buffer, sizeof(buffer));
    JsonWriterString(&writer, "0123456789");
    CHECK_FALSE(JsonWriterOk(&writer));
    /* The buffer holds a NUL-terminated prefix, never a torn value: the writer
     * keeps one byte for the terminator, so seven characters fit in eight. */
    CHECK_INT(7, (int)strlen(buffer));
    CHECK_INT(0, strcmp(buffer, "\"012345"));

    TEST_CASE("once the buffer is full every later write is dropped");
    JsonWriterRaw(&writer, "x");
    CHECK_FALSE(JsonWriterOk(&writer));
    CHECK_INT(7, (int)strlen(buffer));

    TEST_CASE("a writer with no room reports failure immediately");
    JsonWriterInit(&writer, buffer, 0U);
    CHECK_FALSE(JsonWriterOk(&writer));
    JsonWriterRaw(&writer, "x");
    CHECK_FALSE(JsonWriterOk(&writer));

    TEST_CASE("a write that exactly fills the buffer minus the terminator succeeds");
    JsonWriterInit(&writer, buffer, 4U);
    JsonWriterRaw(&writer, "abc");
    CHECK_TRUE(JsonWriterOk(&writer));
    CHECK_INT(0, strcmp(buffer, "abc"));
    JsonWriterRaw(&writer, "d");
    CHECK_FALSE(JsonWriterOk(&writer));
}

/* Entry point for the json_writer suite. */
void test_json_writer_suite(void)
{
    test_basic_output();
    test_value_kinds();
    test_string_escaping();
    test_overflow();
}
