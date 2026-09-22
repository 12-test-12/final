package org.example.client_kmp.monitoring

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertTrue

class TrendChartGeometryTest {

    private fun samplePoint(
        key: String = "k1",
        receivedAt: String = "2026-09-21T10:00:00Z",
        timeText: String = "10:00:00",
        temp: Double = 25.0,
        hum: Double = 50.0,
        gas: Double? = 30.0,
        epochMs: Long = 1000L,
    ): TrendPointView = TrendPointView(
        key = key,
        receivedAt = receivedAt,
        timeText = timeText,
        temperatureText = temp.toInt().toString(),
        humidityText = hum.toInt().toString(),
        gasText = gas?.toInt()?.toString() ?: "--",
        localAlarm = false,
        timestampEpochMs = epochMs,
        temperatureC = temp,
        humidityRh = hum,
        gasPpm = gas,
    )

    @Test
    fun zeroSamplesYieldsEmptyLayoutWithoutCrash() {
        val layout = TrendChartGeometry.compute(emptyList(), width = 300f, height = 200f)
        assertFalse(layout.hasData)
        assertTrue(layout.temperatureSeries.segments.isEmpty())
        assertTrue(layout.temperatureSeries.singlePoints.isEmpty())
        assertTrue(layout.humiditySeries.segments.isEmpty())
        assertTrue(layout.gasSeries.segments.isEmpty())
        assertEquals("--", layout.startTimeText)
        assertEquals("--", layout.endTimeText)
    }

    @Test
    fun singleSamplePlacesPointAtCenter() {
        val pt = samplePoint(temp = 25.0, hum = 50.0, gas = 40.0, epochMs = 1000L)
        val layout = TrendChartGeometry.compute(listOf(pt), width = 300f, height = 200f)
        assertTrue(layout.hasData)
        assertEquals("10:00:00", layout.startTimeText)
        assertEquals("10:00:00", layout.endTimeText)

        // For 1 sample, segments should be empty and singlePoints should have 1 point
        assertEquals(1, layout.temperatureSeries.singlePoints.size)
        assertEquals(1, layout.humiditySeries.singlePoints.size)
        assertEquals(1, layout.gasSeries.singlePoints.size)

        val tempPt = layout.temperatureSeries.singlePoints[0]
        assertEquals(150f, tempPt.x, 0.1f, "Single point X should be centered")
        assertEquals(100f, tempPt.y, 0.1f, "Single point Y should be centered")
    }

    @Test
    fun multipleNormalSamplesAreProportionallyMapped() {
        val pts = listOf(
            samplePoint(key = "p1", timeText = "10:00:00", temp = 20.0, hum = 40.0, gas = 10.0, epochMs = 1000L),
            samplePoint(key = "p2", timeText = "10:30:00", temp = 30.0, hum = 50.0, gas = 20.0, epochMs = 3000L),
            samplePoint(key = "p3", timeText = "11:00:00", temp = 40.0, hum = 60.0, gas = 30.0, epochMs = 5000L),
        )
        val layout = TrendChartGeometry.compute(pts, width = 400f, height = 200f, padLeft = 0f, padRight = 0f, padTop = 0f, padBottom = 0f)
        assertTrue(layout.hasData)
        assertEquals("10:00:00", layout.startTimeText)
        assertEquals("11:00:00", layout.endTimeText)

        val tempSeg = layout.temperatureSeries.segments.single()
        assertEquals(3, tempSeg.size)
        assertEquals(0f, tempSeg[0].x, 0.1f)
        assertEquals(200f, tempSeg[1].x, 0.1f, "10:30 is halfway between 10:00 and 11:00")
        assertEquals(400f, tempSeg[2].x, 0.1f)

        // Temperature increases: 20 -> 30 -> 40, so Y should decrease (higher value = smaller Y in canvas)
        assertTrue(tempSeg[0].y > tempSeg[1].y, "Lower temp has higher Y")
        assertTrue(tempSeg[1].y > tempSeg[2].y, "Higher temp has lower Y")
    }

    @Test
    fun constantSeriesDrawsHorizontalLineAtVerticalCenter() {
        val pts = listOf(
            samplePoint(key = "p1", temp = 25.0, hum = 60.0, gas = 100.0, epochMs = 1000L),
            samplePoint(key = "p2", temp = 25.0, hum = 60.0, gas = 100.0, epochMs = 2000L),
            samplePoint(key = "p3", temp = 25.0, hum = 60.0, gas = 100.0, epochMs = 3000L),
        )
        val layout = TrendChartGeometry.compute(pts, width = 300f, height = 200f, padTop = 20f, padBottom = 20f)
        val tempSeg = layout.temperatureSeries.segments.single()
        val expectedCenterY = (20f + (200f - 20f)) / 2f // 100f
        for (p in tempSeg) {
            assertEquals(expectedCenterY, p.y, 0.01f, "Constant series should be at vertical center")
        }
    }

    @Test
    fun allGasNullYieldsEmptyGasSegmentsWithoutAffectingTempAndHum() {
        val pts = listOf(
            samplePoint(key = "p1", temp = 20.0, hum = 40.0, gas = null, epochMs = 1000L),
            samplePoint(key = "p2", temp = 25.0, hum = 50.0, gas = null, epochMs = 2000L),
        )
        val layout = TrendChartGeometry.compute(pts, width = 300f, height = 200f)
        assertTrue(layout.temperatureSeries.segments.isNotEmpty())
        assertTrue(layout.humiditySeries.segments.isNotEmpty())
        assertTrue(layout.gasSeries.segments.isEmpty())
        assertTrue(layout.gasSeries.singlePoints.isEmpty())
        assertEquals(null, layout.gasSeries.minVal)
        assertEquals(null, layout.gasSeries.maxVal)
    }

    @Test
    fun partialGasNullBreaksLineIntoSegments() {
        val pts = listOf(
            samplePoint(key = "p1", gas = 10.0, epochMs = 1000L),
            samplePoint(key = "p2", gas = 20.0, epochMs = 2000L),
            samplePoint(key = "p3", gas = null, epochMs = 3000L), // missing
            samplePoint(key = "p4", gas = 30.0, epochMs = 4000L),
            samplePoint(key = "p5", gas = 40.0, epochMs = 5000L),
        )
        val layout = TrendChartGeometry.compute(pts, width = 300f, height = 200f)
        val gasSegments = layout.gasSeries.segments
        assertEquals(2, gasSegments.size, "Missing point should break line into 2 segments")
        assertEquals(2, gasSegments[0].size)
        assertEquals(2, gasSegments[1].size)
        assertEquals(10.0, gasSegments[0][0].rawValue)
        assertEquals(20.0, gasSegments[0][1].rawValue)
        assertEquals(30.0, gasSegments[1][0].rawValue)
        assertEquals(40.0, gasSegments[1][1].rawValue)
    }

    @Test
    fun legitimateGasZeroIsNotTreatedAsMissing() {
        val pts = listOf(
            samplePoint(key = "p1", gas = 10.0, epochMs = 1000L),
            samplePoint(key = "p2", gas = 0.0, epochMs = 2000L), // legitimate 0
            samplePoint(key = "p3", gas = 20.0, epochMs = 3000L),
        )
        val layout = TrendChartGeometry.compute(pts, width = 300f, height = 200f)
        val gasSegments = layout.gasSeries.segments
        assertEquals(1, gasSegments.size, "Legitimate 0.0 must not break line segment")
        assertEquals(3, gasSegments[0].size)
        assertEquals(0.0, gasSegments[0][1].rawValue)
    }

    @Test
    fun duplicateTimestampsFallbackToUniformSpacingWithoutCrashing() {
        val pts = listOf(
            samplePoint(key = "p1", epochMs = 1000L),
            samplePoint(key = "p2", epochMs = 1000L),
            samplePoint(key = "p3", epochMs = 1000L),
        )
        val layout = TrendChartGeometry.compute(pts, width = 300f, height = 200f)
        val tempSeg = layout.temperatureSeries.segments.single()
        assertEquals(3, tempSeg.size)
        // Check no NaN
        for (p in tempSeg) {
            assertFalse(p.x.isNaN())
            assertFalse(p.y.isNaN())
        }
    }

    @Test
    fun abnormalTimestampsFallbackToUniformSpacing() {
        // Inverted timestamps (t2 < t1)
        val pts = listOf(
            samplePoint(key = "p1", epochMs = 5000L),
            samplePoint(key = "p2", epochMs = 3000L),
            samplePoint(key = "p3", epochMs = 1000L),
        )
        val layout = TrendChartGeometry.compute(pts, width = 300f, height = 200f)
        val tempSeg = layout.temperatureSeries.segments.single()
        assertEquals(3, tempSeg.size)
        for (p in tempSeg) {
            assertFalse(p.x.isNaN())
            assertFalse(p.y.isNaN())
        }
    }

    @Test
    fun tinyOrZeroCanvasDimensionsDoNotProduceNaNOrInfinity() {
        val pts = listOf(samplePoint(epochMs = 1000L), samplePoint(epochMs = 2000L))
        val layoutZero = TrendChartGeometry.compute(pts, width = 0f, height = 0f)
        assertFalse(layoutZero.hasData)

        val layoutTiny = TrendChartGeometry.compute(pts, width = 1f, height = 1f)
        assertTrue(layoutTiny.hasData)
        for (p in layoutTiny.temperatureSeries.segments.flatten()) {
            assertFalse(p.x.isNaN())
            assertFalse(p.x.isInfinite())
            assertFalse(p.y.isNaN())
            assertFalse(p.y.isInfinite())
        }
    }

    @Test
    fun coordinatesAreStrictlyContainedWithinPlotArea() {
        val pts = listOf(
            samplePoint(temp = -100.0, hum = -50.0, gas = -10.0, epochMs = 1000L),
            samplePoint(temp = 1000.0, hum = 500.0, gas = 5000.0, epochMs = 2000L),
        )
        val width = 400f
        val height = 250f
        val padTop = 15f
        val padBottom = 20f
        val layout = TrendChartGeometry.compute(pts, width = width, height = height, padTop = padTop, padBottom = padBottom)
        for (p in layout.temperatureSeries.segments.flatten()) {
            assertTrue(p.x in 0f..width, "x ${p.x} should be in [0, $width]")
            assertTrue(p.y in padTop..(height - padBottom), "y ${p.y} should be in [$padTop, ${height - padBottom}]")
        }
    }

    @Test
    fun isolatedSinglePointsBetweenNullsAreReportedAsSinglePoints() {
        val pts = listOf(
            samplePoint(key = "p1", gas = null, epochMs = 1000L),
            samplePoint(key = "p2", gas = 50.0, epochMs = 2000L), // isolated
            samplePoint(key = "p3", gas = null, epochMs = 3000L),
        )
        val layout = TrendChartGeometry.compute(pts, width = 300f, height = 200f)
        assertEquals(0, layout.gasSeries.segments.size)
        assertEquals(1, layout.gasSeries.singlePoints.size)
        assertEquals(50.0, layout.gasSeries.singlePoints[0].rawValue)
    }

    @Test
    fun metricRangeFormatting() {
        val rangeNormal = MetricRange(20.4, 35.6)
        assertEquals("20~36 °C", rangeNormal.formatRange("°C"))

        val rangeNull = MetricRange(null, null)
        assertEquals("--", rangeNull.formatRange("ppm"))
    }
}
