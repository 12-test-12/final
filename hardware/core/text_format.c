#include "text_format.h"

/* A 32-bit value has at most ten decimal digits, so a field of that width plus
 * a terminator is always enough for the integer part of a scaled value. */
#define TEXT_MAX_DIGITS 10U

/* The scaled value is a 32-bit integer, so nine fraction digits already consume
 * all of it; a tenth would overflow the divisor. */
#define TEXT_MAX_FRACTION_DIGITS 9U

/* Number of decimal digits `value` needs, at least one for zero. */
static uint8_t digit_count(uint32_t value)
{
    uint8_t digits = 1U;

    while (value >= 10U)
    {
        value /= 10U;
        digits++;
    }
    return digits;
}

/* Shared implementation of the padded decimal formatters. `pad` is the
 * character written to the left of a value that needs fewer than `min_digits`
 * characters. */
static bool format_padded(char *buffer, uint32_t capacity, uint32_t value, uint8_t min_digits, char pad)
{
    uint8_t digits;
    uint8_t index;
    uint8_t width;

    if (buffer == NULL || capacity == 0U)
    {
        return false;
    }

    digits = digit_count(value);
    width = digits > min_digits ? digits : min_digits;
    if ((uint32_t)width + 1U > capacity)
    {
        /* Not enough room for the field plus the terminator. Emit nothing rather
         * than a partial number: a truncated number on a display is worse than
         * an empty field, because it looks like a reading. */
        buffer[0] = '\0';
        return false;
    }

    /* Filling from the right keeps the digit order correct without a reverse
     * pass or a scratch buffer. */
    buffer[width] = '\0';
    for (index = 0U; index < width; index++)
    {
        uint8_t position = (uint8_t)(width - 1U - index);

        if (index < digits)
        {
            buffer[position] = (char)('0' + (char)(value % 10U));
            value /= 10U;
        }
        else
        {
            buffer[position] = pad;
        }
    }
    return true;
}

bool TextFormatUnsigned(char *buffer, uint32_t capacity, uint32_t value, uint8_t min_digits)
{
    return format_padded(buffer, capacity, value, min_digits, ' ');
}

bool TextFormatZeroPadded(char *buffer, uint32_t capacity, uint32_t value, uint8_t min_digits)
{
    return format_padded(buffer, capacity, value, min_digits, '0');
}

bool TextFormatSigned(char *buffer, uint32_t capacity, int32_t value, uint8_t min_digits)
{
    bool negative = value < 0;
    /* INT32_MIN has no positive counterpart, so the magnitude is taken in
     * unsigned arithmetic where it is representable: the two's-complement
     * pattern of INT32_MIN is exactly 2147483648. */
    uint32_t magnitude = negative ? ((uint32_t)0U - (uint32_t)value) : (uint32_t)value;
    uint8_t digits = digit_count(magnitude);
    /* The sign is written immediately before the digits rather than in the
     * padding, so the field reads "  -40" and not "-  40": the latter looks like
     * two separate fields on a display. */
    uint8_t body = (uint8_t)(digits + (negative ? 1U : 0U));
    uint8_t width = body > min_digits ? body : min_digits;
    uint8_t index;

    if (buffer == NULL || capacity == 0U)
    {
        return false;
    }
    if ((uint32_t)width + 1U > capacity)
    {
        buffer[0] = '\0';
        return false;
    }

    buffer[width] = '\0';
    for (index = 0U; index < width; index++)
    {
        uint8_t position = (uint8_t)(width - 1U - index);

        if (index < digits)
        {
            buffer[position] = (char)('0' + (char)(magnitude % 10U));
            magnitude /= 10U;
        }
        else if (negative && index == digits)
        {
            buffer[position] = '-';
        }
        else
        {
            buffer[position] = ' ';
        }
    }
    return true;
}

bool TextFormatScaled(char *buffer, uint32_t capacity, int32_t scaled_value, uint8_t fraction_digits, uint8_t min_int_digits)
{
    uint32_t divisor = 1U;
    uint32_t magnitude;
    uint32_t integer_part;
    uint32_t fraction_part;
    uint8_t index;
    bool negative = scaled_value < 0;
    uint32_t position = 0U;
    char integer_text[TEXT_MAX_DIGITS + 2U];
    char fraction_text[TEXT_MAX_DIGITS + 2U];
    uint32_t integer_length = 0U;

    /* The two conditions are checked separately: writing the empty string on
     * the way out would dereference a null buffer, which is precisely the case
     * the caller asked to be protected from. */
    if (buffer == NULL)
    {
        return false;
    }
    if (capacity == 0U)
    {
        return false;
    }
    if (fraction_digits == 0U)
    {
        return TextFormatSigned(buffer, capacity, scaled_value, min_int_digits);
    }
    /* Both bounds are checked up front so the scratch buffers below are always
     * large enough. An unchecked width would let the padding run past them,
     * which is a stack overflow rather than a formatting glitch. */
    if (fraction_digits > TEXT_MAX_FRACTION_DIGITS || min_int_digits > TEXT_MAX_DIGITS)
    {
        buffer[0] = '\0';
        return false;
    }

    for (index = 0U; index < fraction_digits; index++)
    {
        divisor *= 10U;
    }

    magnitude = negative ? ((uint32_t)0U - (uint32_t)scaled_value) : (uint32_t)scaled_value;
    integer_part = magnitude / divisor;
    fraction_part = magnitude % divisor;

    if (!TextFormatZeroPadded(integer_text, sizeof(integer_text), integer_part, min_int_digits))
    {
        buffer[0] = '\0';
        return false;
    }
    if (!TextFormatZeroPadded(fraction_text, sizeof(fraction_text), fraction_part, fraction_digits))
    {
        buffer[0] = '\0';
        return false;
    }

    if (negative && (integer_part != 0U || fraction_part != 0U))
    {
        if (capacity < 2U)
        {
            buffer[0] = '\0';
            return false;
        }
        buffer[position++] = '-';
    }

    while (integer_text[integer_length] != '\0')
    {
        if (position + 1U >= capacity)
        {
            buffer[0] = '\0';
            return false;
        }
        buffer[position++] = integer_text[integer_length++];
    }
    if (position + 1U >= capacity)
    {
        buffer[0] = '\0';
        return false;
    }
    buffer[position++] = '.';

    for (index = 0U; fraction_text[index] != '\0'; index++)
    {
        if (position + 1U >= capacity)
        {
            buffer[0] = '\0';
            return false;
        }
        buffer[position++] = fraction_text[index];
    }

    buffer[position] = '\0';
    return true;
}

const char *TextSelect(bool condition, const char *when_true, const char *when_false)
{
    return condition ? when_true : when_false;
}
