#ifndef __JSON_WRITER_H
#define __JSON_WRITER_H

/*
 * Minimal JSON writer for the device payloads.
 *
 * The firmware links with -nostdlib, so there is no printf, no allocation and no
 * string library. This writer appends to a caller-owned buffer, escapes the
 * characters JSON requires, and records overflow instead of writing past the
 * end. A caller checks JsonWriterOk once at the end rather than after every
 * call, which keeps the payload builders readable.
 *
 * The writer produces a payload, not a document: it does not validate the
 * sequence of tokens. The host tests parse the output to check that.
 */

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

/* Append-only writer over a fixed buffer. */
typedef struct
{
    char *buffer;
    uint32_t capacity;
    uint32_t length;
    /* Set when anything did not fit. The buffer always holds a NUL-terminated
     * prefix of the intended output, never a truncated mid-token string. */
    bool overflow;
} JsonWriter;

/* Initialise a writer over `buffer`. `capacity` includes the terminator. */
void JsonWriterInit(JsonWriter *writer, char *buffer, uint32_t capacity);

/* Report whether everything written so far fit. */
bool JsonWriterOk(const JsonWriter *writer);

/* Report the number of characters written, excluding the terminator. */
uint32_t JsonWriterLength(const JsonWriter *writer);

/* Append already-valid JSON text, such as a brace or comma. */
void JsonWriterRaw(JsonWriter *writer, const char *text);

/* Append a JSON string with the required escapes applied. Control characters
 * below 0x20 are escaped; a NUL byte cannot appear in a C string and is not
 * representable, which is fine because every value written here is text. */
void JsonWriterString(JsonWriter *writer, const char *text);

/* Append an unsigned integer. */
void JsonWriterUnsigned(JsonWriter *writer, uint32_t value);

/* Append a signed integer. */
void JsonWriterSigned(JsonWriter *writer, int32_t value);

/* Append a fixed-point number with one fraction digit, scaled by ten. */
void JsonWriterScaled1(JsonWriter *writer, int32_t scaled_value);

/* Append `true` or `false`. */
void JsonWriterBool(JsonWriter *writer, bool value);

/* Append `null`. */
void JsonWriterNull(JsonWriter *writer);

/* Append a quoted object key followed by a colon. */
void JsonWriterKey(JsonWriter *writer, const char *key);

#endif /* __JSON_WRITER_H */
