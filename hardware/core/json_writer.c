#include "json_writer.h"
#include "text_format.h"

/* Append one character. Overflow is sticky: once the buffer is full every later
 * write is dropped, so the output is a valid prefix rather than a mixture. */
static void append_char(JsonWriter *writer, char value)
{
    if (writer->overflow)
    {
        return;
    }
    /* One byte is always reserved for the terminator, so a caller that checks
     * JsonWriterOk gets a NUL-terminated string even after an overflow. */
    if (writer->length + 1U >= writer->capacity)
    {
        writer->overflow = true;
        return;
    }
    writer->buffer[writer->length] = value;
    writer->length++;
    writer->buffer[writer->length] = '\0';
}

/* Append a NUL-terminated string verbatim. */
static void append_text(JsonWriter *writer, const char *text)
{
    uint32_t index = 0U;

    while (text[index] != '\0')
    {
        append_char(writer, text[index]);
        index++;
    }
}

/* Append the escaped form of one character of a JSON string. */
static void append_escaped(JsonWriter *writer, char value)
{
    switch (value)
    {
    case '"':
        append_text(writer, "\\\"");
        return;
    case '\\':
        append_text(writer, "\\\\");
        return;
    case '\n':
        append_text(writer, "\\n");
        return;
    case '\r':
        append_text(writer, "\\r");
        return;
    case '\t':
        append_text(writer, "\\t");
        return;
    default:
        break;
    }

    if ((unsigned char)value < 0x20U)
    {
        /* The remaining control characters have no short escape. \u00XX is the
         * portable form and keeps the payload valid for any parser. */
        static const char hex[] = "0123456789abcdef";
        unsigned char code = (unsigned char)value;

        append_text(writer, "\\u00");
        append_char(writer, hex[(code >> 4) & 0x0FU]);
        append_char(writer, hex[code & 0x0FU]);
        return;
    }
    append_char(writer, value);
}

void JsonWriterInit(JsonWriter *writer, char *buffer, uint32_t capacity)
{
    writer->buffer = buffer;
    writer->capacity = capacity;
    writer->length = 0U;
    writer->overflow = capacity == 0U;
    if (capacity > 0U)
    {
        buffer[0] = '\0';
    }
}

bool JsonWriterOk(const JsonWriter *writer)
{
    return !writer->overflow;
}

uint32_t JsonWriterLength(const JsonWriter *writer)
{
    return writer->length;
}

void JsonWriterRaw(JsonWriter *writer, const char *text)
{
    append_text(writer, text);
}

void JsonWriterString(JsonWriter *writer, const char *text)
{
    uint32_t index = 0U;

    if (text == NULL)
    {
        /* A null C string means "no value", which in JSON is null rather than
         * the four-letter string. Writing "" instead would claim the device has
         * an empty name. */
        JsonWriterNull(writer);
        return;
    }

    append_char(writer, '"');
    while (text[index] != '\0')
    {
        append_escaped(writer, text[index]);
        index++;
    }
    append_char(writer, '"');
}

void JsonWriterUnsigned(JsonWriter *writer, uint32_t value)
{
    char text[12];

    if (!TextFormatUnsigned(text, sizeof(text), value, 0U))
    {
        writer->overflow = true;
        return;
    }
    append_text(writer, text);
}

void JsonWriterSigned(JsonWriter *writer, int32_t value)
{
    char text[12];

    if (!TextFormatSigned(text, sizeof(text), value, 0U))
    {
        writer->overflow = true;
        return;
    }
    append_text(writer, text);
}

void JsonWriterScaled1(JsonWriter *writer, int32_t scaled_value)
{
    char text[16];

    if (!TextFormatScaled(text, sizeof(text), scaled_value, 1U, 1U))
    {
        writer->overflow = true;
        return;
    }
    append_text(writer, text);
}

void JsonWriterBool(JsonWriter *writer, bool value)
{
    append_text(writer, value ? "true" : "false");
}

void JsonWriterNull(JsonWriter *writer)
{
    append_text(writer, "null");
}

void JsonWriterKey(JsonWriter *writer, const char *key)
{
    JsonWriterString(writer, key);
    append_char(writer, ':');
}
