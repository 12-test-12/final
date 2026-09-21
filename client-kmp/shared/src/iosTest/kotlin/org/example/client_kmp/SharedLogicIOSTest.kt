package org.example.client_kmp

import org.example.client_kmp.monitoring.AlertState
import org.example.client_kmp.monitoring.Connectivity
import org.example.client_kmp.monitoring.DeviceStatus
import org.example.client_kmp.monitoring.MonitoringPresentation
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse

/**
 * iOS-side check that the shared runtime is reachable and behaves identically on
 * the Kotlin/Native target.
 *
 * The iOS app is not part of this round's verification (the current macOS
 * toolchain cannot build it), so this is intentionally a thin assertion on the
 * shared rules rather than a claim that the iOS UI was exercised. It exists so
 * the target keeps a real, compiling test instead of a placeholder.
 */
class SharedLogicIOSTest {

    @Test
    fun theSharedDashboardRulesAreReachableFromTheNativeTarget() {
        val view = MonitoringPresentation.dashboard(
            DeviceStatus(
                deviceId = "MCU001",
                connectivity = Connectivity.offline,
                alarmState = AlertState.fire_warning,
                lastSeenAt = "2026-09-21T09:00:00Z",
            ),
            telemetry = null,
        )

        assertEquals("火情预警", view.riskText)
        assertEquals("离线", view.connectivityText)
        assertFalse(view.hasData)
        assertEquals("--", view.temperatureText)
    }
}
