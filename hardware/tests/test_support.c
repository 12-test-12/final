#include "test_support.h"

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
