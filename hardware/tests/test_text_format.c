#include "test_support.h"
#include "text_format.h"

/* Exercise the unsigned formatter. */
static void test_unsigned(void)
{
    char buffer[16];

    TEST_CASE("a plain number is written without padding");
    CHECK_TRUE(TextFormatUnsigned(buffer, sizeof(buffer), 42U, 0U));
    CHECK_INT(0, strcmp(buffer, "42"));

    TEST_CASE("zero is written as a single digit");
    CHECK_TRUE(TextFormatUnsigned(buffer, sizeof(buffer), 0U, 0U));
    CHECK_INT(0, strcmp(buffer, "0"));

    TEST_CASE("a minimum field width pads on the left with spaces");
    CHECK_TRUE(TextFormatUnsigned(buffer, sizeof(buffer), 25U, 3U));
    CHECK_INT(0, strcmp(buffer, " 25"));
    /* The sign sits next to the digits, never in the padding. */
    CHECK_TRUE(TextFormatSigned(buffer, sizeof(buffer), -5, 4U));
    CHECK_INT(0, strcmp(buffer, "  -5"));

    TEST_CASE("a value wider than the field is not truncated");
    CHECK_TRUE(TextFormatUnsigned(buffer, sizeof(buffer), 4095U, 2U));
    CHECK_INT(0, strcmp(buffer, "4095"));

    TEST_CASE("the largest 16-bit value fits");
    CHECK_TRUE(TextFormatUnsigned(buffer, sizeof(buffer), 65535U, 1U));
    CHECK_INT(0, strcmp(buffer, "65535"));

    TEST_CASE("the largest 32-bit value fits");
    CHECK_TRUE(TextFormatUnsigned(buffer, sizeof(buffer), 4294967295U, 1U));
    CHECK_INT(0, strcmp(buffer, "4294967295"));

    TEST_CASE("bounds every power of ten");
    CHECK_TRUE(TextFormatUnsigned(buffer, sizeof(buffer), 9U, 0U));
    CHECK_INT(0, strcmp(buffer, "9"));
    CHECK_TRUE(TextFormatUnsigned(buffer, sizeof(buffer), 10U, 0U));
    CHECK_INT(0, strcmp(buffer, "10"));
    CHECK_TRUE(TextFormatUnsigned(buffer, sizeof(buffer), 99U, 0U));
    CHECK_INT(0, strcmp(buffer, "99"));
    CHECK_TRUE(TextFormatUnsigned(buffer, sizeof(buffer), 100U, 0U));
    CHECK_INT(0, strcmp(buffer, "100"));

    TEST_CASE("an insufficient buffer reports failure and writes nothing");
    /* A truncated number on a panel looks like a reading, so the formatter
     * refuses instead of emitting a plausible wrong value. */
    buffer[0] = 'x';
    CHECK_FALSE(TextFormatUnsigned(buffer, 3U, 4095U, 0U));
    CHECK_INT(0, strcmp(buffer, ""));

    TEST_CASE("a buffer of exactly the required size is accepted");
    CHECK_TRUE(TextFormatUnsigned(buffer, 5U, 4095U, 0U));
    CHECK_INT(0, strcmp(buffer, "4095"));

    TEST_CASE("a null buffer is refused rather than dereferenced");
    CHECK_FALSE(TextFormatUnsigned(NULL, 16U, 1U, 0U));
    CHECK_FALSE(TextFormatSigned(NULL, 16U, 1, 0U));
    CHECK_FALSE(TextFormatZeroPadded(NULL, 16U, 1U, 0U));

    TEST_CASE("a zero capacity is refused");
    CHECK_FALSE(TextFormatUnsigned(buffer, 0U, 1U, 0U));
}

/* Exercise the signed formatter. */
static void test_signed(void)
{
    char buffer[16];

    TEST_CASE("a positive value has no sign");
    CHECK_TRUE(TextFormatSigned(buffer, sizeof(buffer), 25, 2U));
    CHECK_INT(0, strcmp(buffer, "25"));

    TEST_CASE("a negative value keeps its sign, and the sign is part of the field");
    CHECK_TRUE(TextFormatSigned(buffer, sizeof(buffer), -40, 3U));
    CHECK_INT(0, strcmp(buffer, "-40"));
    CHECK_TRUE(TextFormatSigned(buffer, sizeof(buffer), -40, 5U));
    CHECK_INT(0, strcmp(buffer, "  -40"));

    TEST_CASE("zero is not negative");
    CHECK_TRUE(TextFormatSigned(buffer, sizeof(buffer), 0, 2U));
    CHECK_INT(0, strcmp(buffer, " 0"));

    TEST_CASE("INT32_MIN is rendered without overflow");
    /* -2147483648 has no positive counterpart in int32; the formatter must use
     * unsigned arithmetic rather than negating. */
    CHECK_TRUE(TextFormatSigned(buffer, sizeof(buffer), -2147483647 - 1, 1U));
    CHECK_INT(0, strcmp(buffer, "-2147483648"));

    TEST_CASE("INT32_MAX is rendered");
    CHECK_TRUE(TextFormatSigned(buffer, sizeof(buffer), 2147483647, 1U));
    CHECK_INT(0, strcmp(buffer, "2147483647"));

    TEST_CASE("too small a buffer is refused");
    CHECK_FALSE(TextFormatSigned(buffer, 1U, 5, 1U));
    CHECK_FALSE(TextFormatSigned(buffer, 0U, 5, 1U));
    /* A wide value needs more room than a narrow one, so the guard has to fire
     * for the value rather than for a fixed size. */
    CHECK_FALSE(TextFormatSigned(buffer, 2U, -2147483647 - 1, 1U));
    CHECK_FALSE(TextFormatSigned(buffer, 3U, -40, 1U));
}

/* Exercise the fixed-point formatter used for the temperature and the gas
 * estimate. */
static void test_scaled(void)
{
    char buffer[20];

    TEST_CASE("one fraction digit");
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 285, 1U, 0U));
    CHECK_INT(0, strcmp(buffer, "28.5"));

    TEST_CASE("a trailing zero fraction digit is kept");
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 280, 1U, 0U));
    CHECK_INT(0, strcmp(buffer, "28.0"));

    TEST_CASE("a leading zero fraction is padded, not spaced");
    /* "1.05" reads as a measurement; "1. 5" does not. */
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 105, 2U, 0U));
    CHECK_INT(0, strcmp(buffer, "1.05"));

    TEST_CASE("two fraction digits are printed exactly");
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 2849, 2U, 0U));
    CHECK_INT(0, strcmp(buffer, "28.49"));
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 2855, 2U, 0U));
    CHECK_INT(0, strcmp(buffer, "28.55"));

    TEST_CASE("the value is printed as given, with no rounding");
    /* The caller owns the precision, so 28.5 with one digit stays 28.5. Rounding
     * here would treat the last kept digit as a discarded one and turn it into
     * 28.6, which is a value the sensor never reported. */
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 285, 1U, 0U));
    CHECK_INT(0, strcmp(buffer, "28.5"));
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 284, 1U, 0U));
    CHECK_INT(0, strcmp(buffer, "28.4"));

    TEST_CASE("a three-digit integer part is printed in full");
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 999, 1U, 0U));
    CHECK_INT(0, strcmp(buffer, "99.9"));
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 299, 1U, 0U));
    CHECK_INT(0, strcmp(buffer, "29.9"));

    TEST_CASE("zero fraction digits behaves like the integer formatter");
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 28, 0U, 2U));
    CHECK_INT(0, strcmp(buffer, "28"));

    TEST_CASE("a negative value keeps its sign");
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), -105, 1U, 0U));
    CHECK_INT(0, strcmp(buffer, "-10.5"));

    TEST_CASE("a negative value smaller than one digit keeps its sign");
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), -4, 1U, 0U));
    CHECK_INT(0, strcmp(buffer, "-0.4"));

    TEST_CASE("zero is not signed");
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 0, 1U, 0U));
    CHECK_INT(0, strcmp(buffer, "0.0"));

    TEST_CASE("a minimum integer width pads with zeros, not spaces");
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 5, 1U, 2U));
    CHECK_INT(0, strcmp(buffer, "00.5"));
    CHECK_TRUE(TextFormatScaled(buffer, sizeof(buffer), 5, 2U, 3U));
    CHECK_INT(0, strcmp(buffer, "000.05"));

    TEST_CASE("more than nine fraction digits is refused");
    /* A tenth fraction digit would overflow the divisor, so the request is
     * rejected rather than producing a wrong number. */
    CHECK_FALSE(TextFormatScaled(buffer, sizeof(buffer), 5, 10U, 0U));
    CHECK_FALSE(TextFormatScaled(buffer, sizeof(buffer), 5, 255U, 0U));

    TEST_CASE("an integer field wider than a 32-bit value is refused");
    /* The scratch buffer is sized for ten digits; a wider field would let the
     * padding run past it. */
    CHECK_FALSE(TextFormatScaled(buffer, sizeof(buffer), 5, 1U, 11U));
    CHECK_FALSE(TextFormatScaled(buffer, sizeof(buffer), 5, 1U, 255U));

    TEST_CASE("a zero capacity is refused");
    CHECK_FALSE(TextFormatScaled(buffer, 0U, 5, 1U, 0U));

    TEST_CASE("a buffer that cannot hold the whole number is refused");
    /* Each of these stops at a different point of the write, which is what makes
     * them worth listing: the guard has to fire wherever the room runs out. */
    CHECK_FALSE(TextFormatScaled(buffer, 2U, 12345, 1U, 0U));
    CHECK_FALSE(TextFormatScaled(buffer, 2U, 5, 1U, 0U));
    CHECK_FALSE(TextFormatScaled(buffer, 2U, -5, 1U, 0U));
    CHECK_FALSE(TextFormatScaled(buffer, 3U, 5, 1U, 0U));
    CHECK_FALSE(TextFormatScaled(buffer, 1U, 5, 1U, 0U));
    /* And the smallest buffer that does fit must succeed: "0.5" plus its
     * terminator is four bytes. */
    CHECK_TRUE(TextFormatScaled(buffer, 4U, 5, 1U, 0U));
    CHECK_INT(0, strcmp(buffer, "0.5"));
    CHECK_TRUE(TextFormatScaled(buffer, 5U, 5, 1U, 0U));
    CHECK_INT(0, strcmp(buffer, "0.5"));

    TEST_CASE("a null buffer is refused");
    CHECK_FALSE(TextFormatScaled(NULL, 8U, 5, 1U, 0U));
}

/* Exercise the zero-padded formatter used by the display fields. */
static void test_zero_padded(void)
{
    char buffer[16];

    TEST_CASE("a value is padded to the requested width with zeros");
    CHECK_TRUE(TextFormatZeroPadded(buffer, sizeof(buffer), 25U, 3U));
    CHECK_INT(0, strcmp(buffer, "025"));
    CHECK_TRUE(TextFormatZeroPadded(buffer, sizeof(buffer), 0U, 3U));
    CHECK_INT(0, strcmp(buffer, "000"));
    CHECK_TRUE(TextFormatZeroPadded(buffer, sizeof(buffer), 4095U, 4U));
    CHECK_INT(0, strcmp(buffer, "4095"));

    TEST_CASE("a value wider than the field is not truncated");
    CHECK_TRUE(TextFormatZeroPadded(buffer, sizeof(buffer), 4095U, 2U));
    CHECK_INT(0, strcmp(buffer, "4095"));

    TEST_CASE("too small a buffer is refused");
    CHECK_FALSE(TextFormatZeroPadded(buffer, 3U, 25U, 3U));
}

/* Exercise the two-string selector. */
static void test_select(void)
{
    TEST_CASE("selects on the condition");
    CHECK_INT(0, strcmp(TextSelect(true, "on", "off"), "on"));
    CHECK_INT(0, strcmp(TextSelect(false, "on", "off"), "off"));
}

/* Entry point for the text_format suite. */
void test_text_format_suite(void)
{
    test_unsigned();
    test_signed();
    test_scaled();
    test_zero_padded();
    test_select();
}
