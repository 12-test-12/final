package org.example.client_kmp.monitoring

import kotlinx.coroutines.delay
import kotlinx.serialization.SerializationException
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

/**
 * Contract-driven client shared by Android and the MiniApp runtime.
 *
 * It owns endpoint paths, serialization, validation, idempotency and the
 * presentation rules; platform code supplies only HTTP and idempotency keys.
 * Every method is a suspend call that returns a fully formatted view and holds
 * no Activity or page lifecycle, so a host may cancel it at any time.
 *
 * Threading: implementations may block internally, so callers must not invoke
 * these methods on a UI thread that cannot suspend.
 */
class MonitoringClient(
    private val platform: MonitoringPlatform,
    baseUrl: String,
    val deviceId: String = "MCU001",
) {
    private val baseUrl = baseUrl.trimEnd('/')
    private val json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        encodeDefaults = true
    }

    /**
     * Loads device state and the newest sample.
     *
     * The contract answers 404 for "latest" when the device has never reported a
     * valid sample. That is an empty state, not a failure, so the dashboard is
     * still built from the device status with `hasData = false`.
     */
    suspend fun loadDashboard(): DashboardView {
        val status: DeviceStatus = get("/api/v1/devices/$deviceId/status")
        val latest: TelemetryPoint? = getOrNull(path = "/api/v1/devices/$deviceId/telemetry/latest", on = 404)
        return MonitoringPresentation.dashboard(status, latest)
    }

    /**
     * Loads recent samples in ascending event-time order.
     *
     * `order=asc` is sent explicitly rather than relying on the server default so
     * a host can never receive newest-first data by accident.
     */
    suspend fun loadTrends(limit: Int = DEFAULT_SAMPLE_LIMIT): TrendsView {
        val safeLimit = limit.coerceIn(1, MAX_SAMPLE_LIMIT)
        val page: TelemetryPage =
            get("/api/v1/devices/$deviceId/telemetry?limit=$safeLimit&order=asc")
        return MonitoringPresentation.trends(page.items)
    }

    /** Loads the newest alert events up to the contract limit. */
    suspend fun loadAlerts(limit: Int = DEFAULT_ALERT_LIMIT): AlertsView {
        val safeLimit = limit.coerceIn(1, MAX_SAMPLE_LIMIT)
        val page: AlertPage = get("/api/v1/devices/$deviceId/alerts?limit=$safeLimit")
        return MonitoringPresentation.alerts(page.items)
    }

    /** Loads desired and device-confirmed threshold state. */
    suspend fun loadSettings(): SettingsView {
        val thresholds: Thresholds = get("/api/v1/devices/$deviceId/thresholds")
        return MonitoringPresentation.settings(thresholds)
    }

    /**
     * Reads the recorded lifecycle of a previously enqueued command.
     *
     * This is the only way to observe a device acknowledgement, since the
     * accepting response is always `pending`.
     */
    suspend fun loadCommandStatus(requestId: String): CommandStatusView {
        val status: CommandStatus = get("/api/v1/devices/$deviceId/commands/$requestId")
        return MonitoringPresentation.commandStatus(status)
    }

    /**
     * Enqueues a buzzer mute or unmute.
     *
     * Muting never clears `localAlarm` and never stops sampling; the returned
     * view is a backend acknowledgement, not a device confirmation.
     */
    suspend fun setMuted(muted: Boolean): CommandStatusView {
        val accepted: CommandAccepted = send(
            path = "/api/v1/devices/$deviceId/commands/mute",
            method = "POST",
            body = json.encodeToString(MuteCommand(muted)),
        )
        return MonitoringPresentation.commandAccepted(accepted)
    }

    /**
     * Validates and enqueues a threshold update.
     *
     * @throws IllegalArgumentException before any network call when a value is
     *   outside the contract range.
     */
    suspend fun updateThresholds(update: ThresholdUpdate): CommandStatusView {
        MonitoringPresentation.validate(update)
        val accepted: CommandAccepted = send(
            path = "/api/v1/devices/$deviceId/thresholds",
            method = "PUT",
            body = json.encodeToString(update),
        )
        return MonitoringPresentation.commandAccepted(accepted)
    }

    /**
     * Polls until the device acknowledgement settles a command.
     *
     * The enqueue response is always `pending`, so this is how a host reaches a
     * terminal outcome. Transport failures, rate limits and server failures are
     * retried because the command may still land after a temporary outage.
     * Client/contract failures are propagated immediately: treating a 401, 404
     * or malformed successful response as "still awaiting" would hide a broken
     * configuration from both hosts.
     *
     * @return the settled view, or null when no terminal state was observed in
     *   [attempts] polls.
     */
    suspend fun awaitCommandOutcome(
        requestId: String,
        attempts: Int = DEFAULT_COMMAND_ATTEMPTS,
        intervalMillis: Long = DEFAULT_COMMAND_INTERVAL_MS,
    ): CommandStatusView? {
        repeat(attempts) {
            delay(intervalMillis)
            val view = try {
                loadCommandStatus(requestId)
            } catch (failure: MonitoringException) {
                if (!failure.isRetryableStatus()) throw failure
                null
            } catch (_: Exception) {
                // Platform transports expose connectivity failures as ordinary
                // exceptions. Retry them within the bounded polling budget.
                null
            }
            if (view != null && view.settled) return view
        }
        return null
    }

    private fun MonitoringException.isRetryableStatus(): Boolean =
        statusCode == 429 || statusCode >= 500

    /** Encodes a dashboard view as plain JSON for non-Kotlin hosts. */
    fun encodeDashboard(value: DashboardView): String = json.encodeToString(value)

    /** Encodes a trends view as plain JSON for non-Kotlin hosts. */
    fun encodeTrends(value: TrendsView): String = json.encodeToString(value)

    /** Encodes an alerts view as plain JSON for non-Kotlin hosts. */
    fun encodeAlerts(value: AlertsView): String = json.encodeToString(value)

    /** Encodes a settings view as plain JSON for non-Kotlin hosts. */
    fun encodeSettings(value: SettingsView): String = json.encodeToString(value)

    /** Encodes a command lifecycle view as plain JSON for non-Kotlin hosts. */
    fun encodeCommandStatus(value: CommandStatusView): String = json.encodeToString(value)

    private suspend inline fun <reified T> get(path: String): T = decode(platform.request(HttpRequest(baseUrl + path)))

    /** Returns null instead of throwing when the backend answers with [on]. */
    private suspend inline fun <reified T> getOrNull(path: String, on: Int): T? {
        val response = platform.request(HttpRequest(baseUrl + path))
        if (response.statusCode == on) return null
        return decode(response)
    }

    private suspend inline fun <reified T> send(path: String, method: String, body: String): T = decode(
        platform.request(
            HttpRequest(
                url = baseUrl + path,
                method = method,
                headers = mapOf(
                    "Content-Type" to "application/json; charset=utf-8",
                    // The contract requires a unique key per control request; repeating
                    // one with a different payload is rejected as version_conflict.
                    "Idempotency-Key" to platform.newIdempotencyKey(),
                ),
                body = body,
            ),
        ),
    )

    /**
     * Maps a response onto [MonitoringException].
     *
     * A non-2xx body is parsed as the backend error envelope so `code`, `message`
     * and the HTTP status survive to the host. When the body is not that envelope
     * (proxy HTML, truncated payload) the status is still reported. A 2xx body
     * that does not match the schema is reported the same way, so a host has a
     * single failure type to handle instead of also catching serialization
     * internals.
     */
    private inline fun <reified T> decode(response: HttpResponse): T {
        if (response.statusCode in 200..299) {
            return try {
                json.decodeFromString(response.body)
            } catch (failure: SerializationException) {
                throw MonitoringException(
                    code = "malformed_response",
                    message = "响应解析失败 (HTTP ${response.statusCode})",
                    statusCode = response.statusCode,
                )
            }
        }
        val error = runCatching { json.decodeFromString<ApiErrorEnvelope>(response.body).error }.getOrNull()
        throw MonitoringException(
            code = error?.code ?: "http_${response.statusCode}",
            message = error?.message ?: "请求失败 (HTTP ${response.statusCode})",
            statusCode = response.statusCode,
        )
    }

    companion object {
        /** Matches the contract's `limit` default. */
        const val DEFAULT_SAMPLE_LIMIT = 60
        const val DEFAULT_ALERT_LIMIT = 50

        /** Contract maximum accepted by the backend query. */
        const val MAX_SAMPLE_LIMIT = 1000

        /** Control-loop budget: ~15 s, long enough for one device reporting cycle. */
        const val DEFAULT_COMMAND_ATTEMPTS = 10
        const val DEFAULT_COMMAND_INTERVAL_MS = 1_500L
    }
}
