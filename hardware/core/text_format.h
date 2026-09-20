#ifndef __TEXT_FORMAT_H
#define __TEXT_FORMAT_H

/*
 * Integer-to-text helpers for the firmware.
 *
 * The firmware links with -nostdlib, so there is no printf family on the device.
 * The display pages and, later, the MQTT payload builder both need to turn
 * numbers into text, so the conversion lives here once instead of being written
 * out at each call site.
 *
 * Nothing here allocates and nothing here reads past the supplied capacity.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

/* Write `value` as decimal digits into `buffer`, right-aligned in a field of at
 * least `min_digits`, and NUL-terminate it.
 *
 * A value that does not fit is truncated to the digits that do fit and the
 * function returns false, so a caller that cares can detect it. Truncating to
 * zeros would silently display a plausible but wrong number, which is worse
 * than showing a short one.
 *
 * Returns true when the whole value was written. `buffer` must have room for
 * min_digits + 1 bytes. */
bool TextFormatUnsigned(char *buffer, uint32_t capacity, uint32_t value, uint8_t min_digits);

/* Write `value` as a signed decimal number, with a leading '-' when negative.
 * Returns true when the whole value was written. */
bool TextFormatSigned(char *buffer, uint32_t capacity, int32_t value, uint8_t min_digits);

/* Write `value` as decimal digits, zero-padded to at least `min_digits`. The
 * column stays steady as the value grows, which is what a measurement field on a
 * fixed-width display needs.
 *
 * Returns true when the whole value was written. */
bool TextFormatZeroPadded(char *buffer, uint32_t capacity, uint32_t value, uint8_t min_digits);

/* Write a fixed-point value with exactly `fraction_digits` digits after the
 * decimal point. The value is pre-scaled by the caller: 28.5 with one fraction
 * digit is passed as 285.
 *
 * The integer part is zero-padded to `min_int_digits`, so 0.5 with two integer
 * digits prints as "00.5". `min_int_digits` may not exceed ten, and
 * `fraction_digits` may not exceed nine; a larger field cannot be produced from
 * a 32-bit scaled value and is refused rather than silently truncated.
 *
 * There is no rounding. The caller owns the precision, so a value passed at the
 * requested precision is printed exactly as given; rounding here would turn
 * 28.5 into 28.6 by treating the last kept digit as a discarded one.
 *
 * `fraction_digits` may be 0, in which case this behaves like
 * TextFormatSigned. Returns true when the whole number was written. */
bool TextFormatScaled(char *buffer, uint32_t capacity, int32_t scaled_value, uint8_t fraction_digits, uint8_t min_int_digits);

/* Write one of two strings depending on `condition`, which keeps the display
 * pages free of repeated ternary expressions at the call sites. */
const char *TextSelect(bool condition, const char *when_true, const char *when_false);

#endif /* __TEXT_FORMAT_H */
