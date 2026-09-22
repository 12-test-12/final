package org.example.client_kmp.monitoring

/**
 * UTC timestamp formatting for the query parameters the contract expects.
 *
 * The contract's history endpoint takes RFC 3339 `from`/`to` values, and the
 * project has no date-time dependency: `kotlinx-datetime` is not on the shared
 * classpath, and adding it would pull a platform date layer into a module whose
 * whole point is to hold nothing but business rules. The conversion from an
 * epoch millisecond count to a calendar date is therefore written out here,
 * where it is total, dependency-free and unit-testable on every target.
 *
 * Only the UTC form is produced. A local-offset timestamp would need the
 * device's timezone, which neither host passes in, and the argument is used for
 * a range query whose bounds are relative to "now" — an offset would only add a
 * way for the two hosts to disagree.
 */
internal object Rfc3339 {

    private const val MILLIS_PER_SECOND = 1_000L
    private const val SECONDS_PER_MINUTE = 60L
    private const val MINUTES_PER_HOUR = 60L
    private const val HOURS_PER_DAY = 24L
    private const val MILLIS_PER_DAY = HOURS_PER_DAY * MINUTES_PER_HOUR * SECONDS_PER_MINUTE * MILLIS_PER_SECOND

    /** Milliseconds in [hours]; used to turn a trend window into a `from` bound. */
    fun hoursToMillis(hours: Int): Long = hours.toLong() * MINUTES_PER_HOUR * SECONDS_PER_MINUTE * MILLIS_PER_SECOND

    /**
     * Renders `millis` since the Unix epoch as `YYYY-MM-DDTHH:MM:SSZ`.
     *
     * Negative values (before 1970) are supported because a device with a wrong
     * clock can legitimately report one, and a formatter that silently mangled it
     * would turn a clock fault into a confusing server error.
     */
    fun utcFromEpochMillis(millis: Long): String {
        val days = floorDiv(millis, MILLIS_PER_DAY)
        val millisOfDay = millis - days * MILLIS_PER_DAY
        val date = civilFromDays(days)
        val hour = millisOfDay / (MINUTES_PER_HOUR * SECONDS_PER_MINUTE * MILLIS_PER_SECOND)
        val minute = (millisOfDay / (SECONDS_PER_MINUTE * MILLIS_PER_SECOND)) % MINUTES_PER_HOUR
        val second = (millisOfDay / MILLIS_PER_SECOND) % SECONDS_PER_MINUTE
        return buildString {
            appendPadded(date.year, 4)
            append('-')
            appendPadded(date.month, 2)
            append('-')
            appendPadded(date.day, 2)
            append('T')
            appendPadded(hour, 2)
            append(':')
            appendPadded(minute, 2)
            append(':')
            appendPadded(second, 2)
            append('Z')
        }
    }

    private data class CivilDate(val year: Long, val month: Long, val day: Long)

    /**
     * Converts a day count since 1970-01-01 into a proleptic Gregorian date.
     *
     * This is Howard Hinnant's `civil_from_days`: it works entirely in integer
     * arithmetic by shifting the year to start in March, which makes the leap day
     * the last day of the year and removes it from the middle of the calculation.
     * Writing it out is cheaper than a dependency and, unlike a hand-rolled
     * month-length table, it is correct for every year rather than for the ones
     * that happen to have been tested.
     */
    private fun civilFromDays(days: Long): CivilDate {
        val shifted = days + 719_468L
        val era = floorDiv(shifted, 146_097L)
        val dayOfEra = shifted - era * 146_097L
        val yearOfEra = (dayOfEra - dayOfEra / 1_460L + dayOfEra / 36_524L - dayOfEra / 146_096L) / 365L
        val year = yearOfEra + era * 400L
        val dayOfYear = dayOfEra - (365L * yearOfEra + yearOfEra / 4L - yearOfEra / 100L)
        val monthPrime = (5L * dayOfYear + 2L) / 153L
        val day = dayOfYear - (153L * monthPrime + 2L) / 5L + 1L
        val month = if (monthPrime < 10L) monthPrime + 3L else monthPrime - 9L
        return CivilDate(
            year = if (month <= 2L) year + 1L else year,
            month = month,
            day = day,
        )
    }

    /** Floors toward negative infinity, which is what splitting a timeline needs. */
    private fun floorDiv(value: Long, divisor: Long): Long {
        val quotient = value / divisor
        return if (value % divisor != 0L && (value xor divisor) < 0L) quotient - 1L else quotient
    }

    private fun StringBuilder.appendPadded(value: Long, width: Int) {
        val text = value.toString()
        repeat(width - text.length) { append('0') }
        append(text)
    }
}
