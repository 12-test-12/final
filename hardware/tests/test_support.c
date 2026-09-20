#include "test_support.h"

#include <stdbool.h>
#include <stdint.h>

int test_checks = 0;
int test_failures = 0;
const char *test_current_file = "";
int test_current_line = 0;

/* Running totals across every suite. */
static int total_checks = 0;
static int total_failures = 0;

void test_record_failure(const char *expression, const char *detail)
{
    test_failures++;
    (void)fprintf(stderr, "FAIL %s:%d: %s", test_current_file, test_current_line, expression);
    if (detail != NULL)
    {
        (void)fprintf(stderr, " (%s)", detail);
    }
    (void)fputc('\n', stderr);
}

void test_run_suite(const char *name, void (*suite)(void))
{
    test_checks = 0;
    test_failures = 0;

    (void)printf("suite: %s\n", name);
    suite();

    total_checks += test_checks;
    total_failures += test_failures;
    (void)printf("  %d checks, %d failures\n", test_checks, test_failures);
}

int test_finish(void)
{
    (void)printf("\ntotal: %d checks, %d failures\n", total_checks, total_failures);
    return total_failures == 0 ? 0 : 1;
}

/* --- JSON checking helpers -------------------------------------------------
 *
 * A compact recursive-descent validator. It is deliberately small: the host
 * tests need to know that the payload builders emit well-formed JSON and that a
 * key is present, not to model the document. */

static void json_skip_whitespace(const char *text, uint32_t length, uint32_t *position)
{
    while (*position < length)
    {
        char value = text[*position];

        if (value != ' ' && value != '\t' && value != '\n' && value != '\r')
        {
            return;
        }
        (*position)++;
    }
}

static bool json_scan_value(const char *text, uint32_t length, uint32_t *position);

/* Scan a string, rejecting an unterminated one and a control character that was
 * not escaped. */
static bool json_scan_string(const char *text, uint32_t length, uint32_t *position)
{
    if (*position >= length || text[*position] != '"')
    {
        return false;
    }
    (*position)++;
    while (*position < length)
    {
        unsigned char value = (unsigned char)text[*position];

        if (value == '"')
        {
            (*position)++;
            return true;
        }
        if (value == '\\')
        {
            (*position)++;
            if (*position >= length)
            {
                return false;
            }
            (*position)++;
            continue;
        }
        if (value < 0x20U)
        {
            /* A raw control character is invalid inside a JSON string; it has to
             * be escaped. */
            return false;
        }
        (*position)++;
    }
    return false;
}

/* Report whether the key that ends at `key_end` (the byte after its colon) has
 * already appeared among the object's earlier keys.
 *
 * A key is a string followed by a colon at the object's own depth, so nested
 * objects and arrays are stepped over rather than searched. Duplicate keys are
 * legal JSON but ambiguous, and the backend's decoder rejects them, so a builder
 * that emitted one would be rejected in production. */
static bool json_key_repeated(const char *text, uint32_t object_start, uint32_t key_start, uint32_t key_end)
{
    uint32_t position = object_start + 1U;
    uint32_t depth = 0U;
    uint32_t candidate_start = key_start + 1U;
    uint32_t candidate_end = key_end - 2U;

    while (position < key_start)
    {
        char value = text[position];

        if (value == '"')
        {
            uint32_t start = position;
            uint32_t after;

            if (!json_scan_string(text, key_start, &position))
            {
                return true;
            }
            after = position;
            json_skip_whitespace(text, key_start, &after);
            if (depth == 0U && after < key_start && text[after] == ':')
            {
                uint32_t earlier_start = start + 1U;
                uint32_t earlier_end = position - 1U;

                if ((earlier_end - earlier_start) == (candidate_end - candidate_start))
                {
                    uint32_t index = 0U;
                    bool equal = true;

                    while (index < (earlier_end - earlier_start))
                    {
                        if (text[earlier_start + index] != text[candidate_start + index])
                        {
                            equal = false;
                            break;
                        }
                        index++;
                    }
                    if (equal)
                    {
                        return true;
                    }
                }
            }
            position = (after > position) ? after : position;
            continue;
        }
        if (value == '{' || value == '[')
        {
            depth++;
        }
        else if (value == '}' || value == ']')
        {
            depth--;
        }
        position++;
    }
    return false;
}

static bool json_scan_object(const char *text, uint32_t length, uint32_t *position)
{
    uint32_t object_start = *position;

    (*position)++;
    json_skip_whitespace(text, length, position);
    if (*position < length && text[*position] == '}')
    {
        (*position)++;
        return true;
    }
    while (true)
    {
        uint32_t key_start = *position;

        if (!json_scan_string(text, length, position))
        {
            return false;
        }
        if (json_key_repeated(text, object_start, key_start, *position))
        {
            return false;
        }
        json_skip_whitespace(text, length, position);
        if (*position >= length || text[*position] != ':')
        {
            return false;
        }
        (*position)++;
        if (!json_scan_value(text, length, position))
        {
            return false;
        }
        json_skip_whitespace(text, length, position);
        if (*position >= length)
        {
            return false;
        }
        if (text[*position] == ',')
        {
            (*position)++;
            continue;
        }
        if (text[*position] == '}')
        {
            (*position)++;
            return true;
        }
        return false;
    }
}

static bool json_scan_array(const char *text, uint32_t length, uint32_t *position)
{
    (*position)++;
    json_skip_whitespace(text, length, position);
    if (*position < length && text[*position] == ']')
    {
        (*position)++;
        return true;
    }
    while (true)
    {
        if (!json_scan_value(text, length, position))
        {
            return false;
        }
        json_skip_whitespace(text, length, position);
        if (*position >= length)
        {
            return false;
        }
        if (text[*position] == ',')
        {
            (*position)++;
            continue;
        }
        if (text[*position] == ']')
        {
            (*position)++;
            return true;
        }
        return false;
    }
}

static bool json_scan_number(const char *text, uint32_t length, uint32_t *position)
{
    uint32_t digits = 0U;

    if (*position < length && text[*position] == '-')
    {
        (*position)++;
    }
    while (*position < length && text[*position] >= '0' && text[*position] <= '9')
    {
        (*position)++;
        digits++;
    }
    if (digits == 0U)
    {
        return false;
    }
    if (*position < length && text[*position] == '.')
    {
        uint32_t fraction = 0U;

        (*position)++;
        while (*position < length && text[*position] >= '0' && text[*position] <= '9')
        {
            (*position)++;
            fraction++;
        }
        if (fraction == 0U)
        {
            return false;
        }
    }
    if (*position < length && (text[*position] == 'e' || text[*position] == 'E'))
    {
        (*position)++;
        if (*position < length && (text[*position] == '+' || text[*position] == '-'))
        {
            (*position)++;
        }
        if (*position >= length || text[*position] < '0' || text[*position] > '9')
        {
            return false;
        }
        while (*position < length && text[*position] >= '0' && text[*position] <= '9')
        {
            (*position)++;
        }
    }
    return true;
}

static bool json_scan_literal(const char *text, uint32_t length, uint32_t *position, const char *literal)
{
    uint32_t index = 0U;

    while (literal[index] != '\0')
    {
        if (*position + index >= length || text[*position + index] != literal[index])
        {
            return false;
        }
        index++;
    }
    *position += index;
    return true;
}

static bool json_scan_value(const char *text, uint32_t length, uint32_t *position)
{
    json_skip_whitespace(text, length, position);
    if (*position >= length)
    {
        return false;
    }
    switch (text[*position])
    {
    case '{':
        return json_scan_object(text, length, position);
    case '[':
        return json_scan_array(text, length, position);
    case '"':
        return json_scan_string(text, length, position);
    case 't':
        return json_scan_literal(text, length, position, "true");
    case 'f':
        return json_scan_literal(text, length, position, "false");
    case 'n':
        return json_scan_literal(text, length, position, "null");
    default:
        return json_scan_number(text, length, position);
    }
}

bool test_json_is_well_formed(const char *text, uint32_t length)
{
    uint32_t position = 0U;

    if (text == NULL)
    {
        return false;
    }
    if (!json_scan_value(text, length, &position))
    {
        return false;
    }
    json_skip_whitespace(text, length, &position);
    return position == length;
}

/* Find `"key":` as a substring. The builders emit no space after the colon, so
 * this is exact for their output. */
static bool json_find_key(const char *text, const char *key, uint32_t *key_end)
{
    uint32_t index = 0U;

    while (text[index] != '\0')
    {
        uint32_t offset = 0U;

        if (text[index] != '"')
        {
            index++;
            continue;
        }
        offset = 0U;
        while (key[offset] != '\0' && text[index + 1U + offset] == key[offset])
        {
            offset++;
        }
        if (key[offset] == '\0' && text[index + 1U + offset] == '"' && text[index + 2U + offset] == ':')
        {
            *key_end = index + 3U + offset;
            return true;
        }
        index++;
    }
    return false;
}

bool test_json_has_key(const char *text, const char *key)
{
    uint32_t key_end = 0U;

    if (text == NULL || key == NULL)
    {
        return false;
    }
    return json_find_key(text, key, &key_end);
}

bool test_json_has_string(const char *text, const char *key, const char *value)
{
    uint32_t key_end = 0U;
    uint32_t offset = 0U;

    if (text == NULL || key == NULL || value == NULL)
    {
        return false;
    }
    if (!json_find_key(text, key, &key_end))
    {
        return false;
    }
    if (text[key_end] != '"')
    {
        return false;
    }
    while (value[offset] != '\0')
    {
        if (text[key_end + 1U + offset] != value[offset])
        {
            return false;
        }
        offset++;
    }
    return text[key_end + 1U + offset] == '"';
}

bool test_json_has_number(const char *text, const char *key, const char *value)
{
    uint32_t key_end = 0U;
    uint32_t offset = 0U;

    if (text == NULL || key == NULL || value == NULL)
    {
        return false;
    }
    if (!json_find_key(text, key, &key_end))
    {
        return false;
    }
    while (value[offset] != '\0')
    {
        if (text[key_end + offset] != value[offset])
        {
            return false;
        }
        offset++;
    }
    return true;
}
