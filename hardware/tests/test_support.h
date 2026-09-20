#ifndef __TEST_SUPPORT_H
#define __TEST_SUPPORT_H

/*
 * Minimal assertion harness for the host-side tests of the pure firmware logic.
 *
 * There is no test framework here on purpose: the firmware toolchain has no
 * package manager, and the alternative — requiring a developer to fetch one —
 * would make the tests optional in practice. This header plus test_support.c is
 * enough to run the suites under CMake, under a bare compiler, and under a
 * coverage tool.
 *
 * A failing check records a message and continues, so one run reports every
 * problem rather than only the first. Each suite returns its failure count so
 * the process exit status reflects the result.
 */

#include <stdio.h>
#include <string.h>

/* Counters for the current suite, defined in test_support.c. */
extern int test_checks;
extern int test_failures;

/* Context of the assertion being evaluated, set by the CHECK macros. */
extern const char *test_current_file;
extern int test_current_line;

/* Record a failed check with a rendered message. */
void test_record_failure(const char *expression, const char *detail);

/* Run one suite, printing a summary line and updating the global totals. */
void test_run_suite(const char *name, void (*suite)(void));

/* Print the overall summary and return the process exit code. */
int test_finish(void);

/* Check a plain condition. */
#define CHECK(condition)                                                       \
    do                                                                         \
    {                                                                          \
        test_checks++;                                                         \
        test_current_file = __FILE__;                                          \
        test_current_line = __LINE__;                                          \
        if (!(condition))                                                      \
        {                                                                      \
            test_record_failure(#condition, NULL);                             \
        }                                                                      \
    } while (0)

/* Check a condition, reporting a rendered detail when it fails. */
#define CHECK_MSG(condition, ...)                                              \
    do                                                                         \
    {                                                                          \
        test_checks++;                                                         \
        test_current_file = __FILE__;                                          \
        test_current_line = __LINE__;                                          \
        if (!(condition))                                                      \
        {                                                                      \
            char test_detail_buffer[256];                                      \
            (void)snprintf(test_detail_buffer, sizeof(test_detail_buffer),     \
                           __VA_ARGS__);                                       \
            test_record_failure(#condition, test_detail_buffer);               \
        }                                                                      \
    } while (0)

/* Check two integers for equality, printing both values on failure. */
#define CHECK_INT(expected, actual)                                            \
    do                                                                         \
    {                                                                          \
        long long test_expected_value = (long long)(expected);                 \
        long long test_actual_value = (long long)(actual);                     \
        test_checks++;                                                         \
        test_current_file = __FILE__;                                          \
        test_current_line = __LINE__;                                          \
        if (test_expected_value != test_actual_value)                          \
        {                                                                      \
            char test_detail_buffer[256];                                      \
            (void)snprintf(test_detail_buffer, sizeof(test_detail_buffer),     \
                           "expected %lld, got %lld",                          \
                           test_expected_value, test_actual_value);            \
            test_record_failure(#expected " == " #actual, test_detail_buffer); \
        }                                                                      \
    } while (0)

/* Check a boolean result. */
#define CHECK_TRUE(condition) CHECK_MSG((condition), "%s is false", #condition)

/* Check a false boolean result. */
#define CHECK_FALSE(condition) CHECK_MSG(!(condition), "%s is true", #condition)

/* Start a named case inside a suite. */
#define TEST_CASE(name) printf("  case: %s\n", name)

#endif /* __TEST_SUPPORT_H */
