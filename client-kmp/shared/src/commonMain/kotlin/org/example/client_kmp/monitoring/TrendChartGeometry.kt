package org.example.client_kmp.monitoring

import kotlin.math.roundToInt

/**
 * One point in chart coordinate space (pixels/dp).
 */
data class ChartPoint(
    val x: Float,
    val y: Float,
    val rawValue: Double,
    val isMissing: Boolean = false,
)

/**
 * A continuous series of points.
 * For temperature and humidity, segments will typically contain 1 list of points.
 * For gas, missing points (null gasPpm) break the line into multiple segments.
 */
data class ChartSeries(
    val metricName: String,
    val tone: String,
    val unit: String,
    val minVal: Double?,
    val maxVal: Double?,
    val segments: List<List<ChartPoint>>,
    val singlePoints: List<ChartPoint>,
)

/**
 * Metric range for legend display.
 */
data class MetricRange(
    val min: Double?,
    val max: Double?,
) {
    fun formatRange(unit: String): String {
        if (min == null || max == null) return "--"
        return "${min.roundToInt()}~${max.roundToInt()} $unit"
    }
}

/**
 * Precomputed layout for drawing the trend chart.
 */
data class TrendChartLayout(
    val width: Float,
    val height: Float,
    val hasData: Boolean,
    val gridLinesY: List<Float>,
    val temperatureSeries: ChartSeries,
    val humiditySeries: ChartSeries,
    val gasSeries: ChartSeries,
    val startX: Float,
    val endX: Float,
    val startTimeText: String,
    val endTimeText: String,
)

object TrendChartGeometry {

    private const val DEFAULT_GRID_COUNT = 4
    private const val PADDING_RATIO = 0.10 // 10% top and bottom padding

    /**
     * Computes chart coordinates for the given points within the canvas bounding box.
     */
    fun compute(
        points: List<TrendPointView>,
        width: Float,
        height: Float,
        padLeft: Float = 0f,
        padRight: Float = 0f,
        padTop: Float = 16f,
        padBottom: Float = 16f,
    ): TrendChartLayout {
        if (points.isEmpty() || width <= 0f || height <= 0f) {
            val emptySeries = { name: String, tone: String, unit: String ->
                ChartSeries(name, tone, unit, null, null, emptyList(), emptyList())
            }
            return TrendChartLayout(
                width = width.coerceAtLeast(0f),
                height = height.coerceAtLeast(0f),
                hasData = false,
                gridLinesY = emptyList(),
                temperatureSeries = emptySeries("温度", Tone.DANGER, "°C"),
                humiditySeries = emptySeries("湿度", Tone.INFO, "%RH"),
                gasSeries = emptySeries("气体", Tone.MINT, "ppm"),
                startX = padLeft,
                endX = (width - padRight).coerceAtLeast(padLeft),
                startTimeText = "--",
                endTimeText = "--",
            )
        }

        val plotLeft = padLeft.coerceIn(0f, width)
        val plotRight = (width - padRight).coerceIn(plotLeft, width)
        val plotTop = padTop.coerceIn(0f, height)
        val plotBottom = (height - padBottom).coerceIn(plotTop, height)
        val plotWidth = (plotRight - plotLeft).coerceAtLeast(1f)
        val plotHeight = (plotBottom - plotTop).coerceAtLeast(1f)

        // 4 horizontal grid lines evenly spaced
        val gridLinesY = if (DEFAULT_GRID_COUNT <= 1) {
            listOf((plotTop + plotBottom) / 2f)
        } else {
            (0 until DEFAULT_GRID_COUNT).map { i ->
                plotTop + (i.toFloat() / (DEFAULT_GRID_COUNT - 1)) * plotHeight
            }
        }

        // X coordinate mapping
        val xCoords = computeXCoordinates(points, plotLeft, plotRight, plotWidth)

        // Compute each metric series
        val tempSeries = computeSeries(
            metricName = "温度",
            tone = Tone.DANGER,
            unit = "°C",
            values = points.map { it.temperatureC },
            xCoords = xCoords,
            plotTop = plotTop,
            plotBottom = plotBottom,
            plotHeight = plotHeight,
        )

        val humSeries = computeSeries(
            metricName = "湿度",
            tone = Tone.INFO,
            unit = "%RH",
            values = points.map { it.humidityRh },
            xCoords = xCoords,
            plotTop = plotTop,
            plotBottom = plotBottom,
            plotHeight = plotHeight,
        )

        val gasSeries = computeSeries(
            metricName = "气体",
            tone = Tone.MINT,
            unit = "ppm",
            values = points.map { it.gasPpm },
            xCoords = xCoords,
            plotTop = plotTop,
            plotBottom = plotBottom,
            plotHeight = plotHeight,
        )

        val startText = points.first().timeText
        val endText = points.last().timeText

        return TrendChartLayout(
            width = width,
            height = height,
            hasData = true,
            gridLinesY = gridLinesY,
            temperatureSeries = tempSeries,
            humiditySeries = humSeries,
            gasSeries = gasSeries,
            startX = plotLeft,
            endX = plotRight,
            startTimeText = startText,
            endTimeText = endText,
        )
    }

    private fun computeXCoordinates(
        points: List<TrendPointView>,
        plotLeft: Float,
        plotRight: Float,
        plotWidth: Float,
    ): List<Float> {
        if (points.size == 1) {
            return listOf((plotLeft + plotRight) / 2f)
        }

        val timestamps = points.map { it.timestampEpochMs }
        val tMin = timestamps.first()
        val tMax = timestamps.last()

        // If timestamps are invalid or non-increasing, fallback to uniform spacing
        if (tMax <= tMin) {
            val step = plotWidth / (points.size - 1)
            return points.indices.map { i -> (plotLeft + i * step).coerceIn(plotLeft, plotRight) }
        }

        val timeSpan = (tMax - tMin).toDouble()
        return timestamps.map { t ->
            val fraction = ((t - tMin).toDouble() / timeSpan).coerceIn(0.0, 1.0)
            (plotLeft + fraction.toFloat() * plotWidth).coerceIn(plotLeft, plotRight)
        }
    }

    private fun computeSeries(
        metricName: String,
        tone: String,
        unit: String,
        values: List<Double?>,
        xCoords: List<Float>,
        plotTop: Float,
        plotBottom: Float,
        plotHeight: Float,
    ): ChartSeries {
        val validValues = values.filterNotNull().filter { it.isFinite() }
        if (validValues.isEmpty()) {
            return ChartSeries(
                metricName = metricName,
                tone = tone,
                unit = unit,
                minVal = null,
                maxVal = null,
                segments = emptyList(),
                singlePoints = emptyList(),
            )
        }

        val minVal = validValues.minOrNull() ?: 0.0
        val maxVal = validValues.maxOrNull() ?: 0.0

        val segments = mutableListOf<List<ChartPoint>>()
        val singlePoints = mutableListOf<ChartPoint>()
        var currentSegment = mutableListOf<ChartPoint>()

        for (i in values.indices) {
            val v = values[i]
            if (v == null || !v.isFinite()) {
                if (currentSegment.isNotEmpty()) {
                    if (currentSegment.size == 1) {
                        singlePoints.add(currentSegment[0])
                    } else {
                        segments.add(currentSegment)
                    }
                    currentSegment = mutableListOf()
                }
            } else {
                val y = computeY(v, minVal, maxVal, plotTop, plotBottom, plotHeight)
                currentSegment.add(ChartPoint(x = xCoords[i], y = y, rawValue = v, isMissing = false))
            }
        }

        if (currentSegment.isNotEmpty()) {
            if (currentSegment.size == 1) {
                singlePoints.add(currentSegment[0])
            } else {
                segments.add(currentSegment)
            }
        }

        return ChartSeries(
            metricName = metricName,
            tone = tone,
            unit = unit,
            minVal = minVal,
            maxVal = maxVal,
            segments = segments,
            singlePoints = singlePoints,
        )
    }

    private fun computeY(
        v: Double,
        minVal: Double,
        maxVal: Double,
        plotTop: Float,
        plotBottom: Float,
        plotHeight: Float,
    ): Float {
        // Constant sequence
        if (maxVal <= minVal) {
            return (plotTop + plotBottom) / 2f
        }

        val span = maxVal - minVal
        val padding = span * PADDING_RATIO
        val paddedMin = minVal - padding
        val paddedMax = maxVal + padding
        val paddedSpan = paddedMax - paddedMin

        val normY = ((v - paddedMin) / paddedSpan).coerceIn(0.0, 1.0)
        // Canvas coordinate: 0 is top, height is bottom
        val y = plotBottom - normY.toFloat() * plotHeight
        return y.coerceIn(plotTop, plotBottom)
    }
}
