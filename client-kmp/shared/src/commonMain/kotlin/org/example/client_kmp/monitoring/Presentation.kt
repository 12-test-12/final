package org.example.client_kmp.monitoring

import kotlinx.serialization.Serializable
import kotlin.math.round

/**
 * Threshold ranges frozen by `docs/api/openapi.yaml`.
 *
 * They are used both for validating an outgoing update and for scaling the
 * dashboard meters, so a meter and its alarm boundary can never disagree.
 */
object ThresholdLimits {
    const val TEMPERATURE_MIN_C = 0.0
    const val TEMPERATURE_MAX_C = 80.0
    const val HUMIDITY_MIN_RH = 0.0
    const val HUMIDITY_MAX_RH = 100.0
    const val GAS_MIN_PPM = 1.0
    const val GAS_MAX_PPM = 999.0
}

/** Shared presentation tone tokens; hosts map them to their own colour system. */
object Tone {
    const val MINT = "mint"
    const val WARNING = "warning"
    const val DANGER = "danger"
    const val INFO = "info"
}

/**
 * Everything the dashboard needs, already formatted and scaled.
 *
 * `hasData` is false when the backend has no valid telemetry yet (the contract
 * answers 404 for "latest" rather than a zero-filled sample). Metrics are then
 * `--` while device and alarm state stay real, so a newly provisioned device
 * shows an empty state instead of an error or a fake reading.
 */
@Serializable
data class DashboardView(
    val deviceId: String,
    val hasData: Boolean,
    val online: Boolean,
    val riskLevel: String,
    val riskTone: String,
    val riskText: String,
    val riskDetail: String,
    val connectivityText: String,
    val temperatureText: String,
    val humidityText: String,
    val gasText: String,
    val gasAvailable: Boolean,
    val temperaturePercent: Int,
    val humidityPercent: Int,
    val gasPercent: Int,
    val localAlarm: Boolean,
    val localAlarmText: String,
    val buzzerText: String,
    val buzzerMuted: Boolean,
    val updatedAt: String,
)

/** Minimum / average / maximum of one metric, pre-formatted for display. */
@Serializable
data class MetricSummary(val minimum: String, val average: String, val maximum: String)

/**
 * One trend row, pre-formatted for list rendering.
 *
 * `key` is always present so a host list can key on a real field: the device
 * ordering tuple when the firmware reports it, and the sample position
 * otherwise. Gas is `--` when the estimate is unavailable, which is different
 * from a measured zero.
 */
@Serializable
data class TrendPointView(
    val key: String,
    val receivedAt: String,
    val timeText: String,
    val temperatureText: String,
    val humidityText: String,
    val gasText: String,
    val localAlarm: Boolean,
)

/**
 * Historical statistics plus the ordered rows they were computed from.
 *
 * `series` keeps the backend's ascending event-time order; hosts must not
 * assume the newest sample is first.
 */
@Serializable
data class TrendsView(
    val sampleCount: Int,
    val hasData: Boolean,
    val temperature: MetricSummary,
    val humidity: MetricSummary,
    val gas: MetricSummary,
    val gasSampleCount: Int,
    val series: List<TrendPointView>,
)

/**
 * One alert rendered for a list.
 *
 * Text lives here rather than in each host so Android and the MiniApp cannot
 * drift into different labels for the same backend state.
 */
@Serializable
data class AlertItemView(
    val id: String,
    val state: String,
    val stateText: String,
    val tone: String,
    val startedAt: String,
    val endedAt: String,
    val active: Boolean,
    val gasAdcRiseText: String,
    val gasAdcRiseThresholdText: String,
    val temperatureRateText: String,
    val temperatureRateThresholdText: String,
    val sampleCountText: String,
    val windowSecondsText: String,
)

/** Alert-list payload shared by Android and MiniApp hosts. */
@Serializable
data class AlertsView(val count: Int, val items: List<AlertItemView>)

/** Threshold settings with device-confirmation wording resolved. */
@Serializable
data class SettingsView(
    val temperatureHighC: Double,
    val humidityHighRh: Double,
    val gasHighPpm: Double,
    val desiredVersion: Int,
    val confirmedVersion: Int?,
    val confirmationState: String,
    val confirmationText: String,
    val confirmationTone: String,
    val confirmed: Boolean,
    val awaitingDevice: Boolean,
    val updatedAt: String,
)

/** Lifecycle wording for a control command; `confirmed` is true only after the device said so. */
@Serializable
data class CommandStatusView(
    val requestId: String,
    val state: String,
    val stateText: String,
    val tone: String,
    val settled: Boolean,
    val confirmed: Boolean,
    val failed: Boolean,
    val versionText: String,
    val errorText: String,
)

/**
 * Pure presentation rules consumed by both platform UIs.
 *
 * Every function is total and side-effect free so it can be unit tested without
 * a platform host. `validate` is the only throwing entry point and reports the
 * contract range it rejected.
 */
object MonitoringPresentation {

    /**
     * Derives the dashboard from device state plus an optional latest sample.
     *
     * `telemetry` is null when the device has no valid sample yet; alarm and
     * mute state then fall back to [DeviceStatus] so the page stays truthful.
     */
    fun dashboard(status: DeviceStatus, telemetry: TelemetryPoint?): DashboardView {
        val risk = when (status.alarmState) {
            AlertState.normal -> Risk("normal", Tone.MINT, "环境正常", "各项指标处于安全范围")
            AlertState.suspect -> Risk("suspect", Tone.WARNING, "疑似异常", "部分条件异常，系统确认中")
            AlertState.fire_warning -> Risk("fire", Tone.DANGER, "火情预警", "气体突增与温升速率同时超限")
            AlertState.recovered -> Risk("recovered", Tone.INFO, "指标已恢复", "事件归档中")
        }
        val localAlarm = telemetry?.localAlarm ?: status.localAlarm
        val muted = telemetry?.buzzerMuted ?: status.buzzerMuted
        return DashboardView(
            deviceId = status.deviceId,
            hasData = telemetry != null,
            online = status.connectivity == Connectivity.online,
            riskLevel = risk.level,
            riskTone = risk.tone,
            riskText = risk.text,
            riskDetail = risk.detail,
            connectivityText = when (status.connectivity) {
                Connectivity.online -> "在线"
                Connectivity.offline -> "离线"
                Connectivity.unknown -> "未知"
            },
            temperatureText = telemetry?.let { decimal(it.temperatureC) } ?: "--",
            humidityText = telemetry?.let { decimal(it.humidityRh) } ?: "--",
            gasText = telemetry?.gasPpm?.let(::decimal) ?: "--",
            gasAvailable = telemetry?.gasPpm != null,
            temperaturePercent = percent(telemetry?.temperatureC, ThresholdLimits.TEMPERATURE_MAX_C),
            humidityPercent = percent(telemetry?.humidityRh, ThresholdLimits.HUMIDITY_MAX_RH),
            gasPercent = percent(telemetry?.gasPpm, ThresholdLimits.GAS_MAX_PPM),
            localAlarm = localAlarm,
            localAlarmText = if (localAlarm) "报警中" else "正常",
            buzzerText = when {
                muted -> "已静音"
                localAlarm -> "报警策略生效"
                else -> "待机"
            },
            buzzerMuted = muted,
            updatedAt = telemetry?.receivedAt ?: status.lastSeenAt ?: "--",
        )
    }

    /**
     * Summarises samples in the order the backend returned them (ascending).
     *
     * Samples without a gas reading are excluded from the gas statistics so an
     * uncalibrated device cannot drag the average toward zero.
     */
    fun trends(points: List<TelemetryPoint>): TrendsView {
        val gasValues = points.mapNotNull { it.gasPpm }
        return TrendsView(
            sampleCount = points.size,
            hasData = points.isNotEmpty(),
            temperature = summarize(points.map { it.temperatureC }),
            humidity = summarize(points.map { it.humidityRh }),
            gas = summarize(gasValues),
            gasSampleCount = gasValues.size,
            series = points.mapIndexed { index, point -> trendPoint(point, index) },
        )
    }

    private fun trendPoint(point: TelemetryPoint, index: Int): TrendPointView = TrendPointView(
        // The device ordering tuple is preferred; the index keeps the key unique
        // when the firmware has not reported bootId/sequence yet.
        key = point.sequence?.let { "${point.bootId ?: "boot"}-$it" } ?: "idx-$index",
        receivedAt = point.receivedAt,
        timeText = clockText(point.receivedAt),
        temperatureText = decimal(point.temperatureC),
        humidityText = decimal(point.humidityRh),
        gasText = point.gasPpm?.let(::decimal) ?: "--",
        localAlarm = point.localAlarm,
    )

    /** Maps persisted alert events without recomputing their trigger evidence. */
    fun alerts(events: List<AlertEvent>): AlertsView = AlertsView(events.size, events.map(::alert))

    /** Maps one persisted event to host-ready labels and formatted evidence. */
    fun alert(event: AlertEvent): AlertItemView {
        val tone = when (event.state) {
            AlertState.fire_warning -> Tone.DANGER
            AlertState.suspect -> Tone.WARNING
            AlertState.recovered -> Tone.INFO
            AlertState.normal -> Tone.MINT
        }
        return AlertItemView(
            id = event.id,
            state = event.state.name,
            stateText = when (event.state) {
                AlertState.fire_warning -> "火情预警"
                AlertState.suspect -> "疑似异常"
                AlertState.recovered -> "已恢复"
                AlertState.normal -> "正常"
            },
            tone = tone,
            startedAt = event.startedAt,
            endedAt = event.endedAt ?: "--",
            active = event.endedAt == null,
            gasAdcRiseText = event.evidence.gasAdcRise.toString(),
            gasAdcRiseThresholdText = event.evidence.gasAdcRiseThreshold?.toString() ?: "--",
            temperatureRateText = "${decimal(event.evidence.temperatureRateCPerMinute)} °C/min",
            temperatureRateThresholdText =
                event.evidence.temperatureRateThresholdCPerMinute?.let { "${decimal(it)} °C/min" } ?: "--",
            sampleCountText = event.evidence.sampleCount.toString(),
            windowSecondsText = event.evidence.windowSeconds?.toString() ?: "--",
        )
    }

    /**
     * Resolves confirmation wording.
     *
     * A desired version above the confirmed version is never reported as
     * confirmed, matching the contract's "never present desired as confirmed".
     */
    fun settings(value: Thresholds): SettingsView {
        val confirmed = value.confirmationState == ConfirmationState.confirmed &&
            value.confirmedVersion != null &&
            value.confirmedVersion >= value.desiredVersion
        return SettingsView(
            temperatureHighC = value.temperatureHighC,
            humidityHighRh = value.humidityHighRh,
            gasHighPpm = value.gasHighPpm,
            desiredVersion = value.desiredVersion,
            confirmedVersion = value.confirmedVersion,
            confirmationState = value.confirmationState.name,
            confirmationText = when (value.confirmationState) {
                ConfirmationState.confirmed -> "设备已确认"
                ConfirmationState.pending -> "等待设备确认"
                ConfirmationState.rejected -> "设备已拒绝"
                ConfirmationState.timed_out -> "确认超时，请重试"
            },
            confirmationTone = when (value.confirmationState) {
                ConfirmationState.confirmed -> Tone.MINT
                ConfirmationState.pending -> Tone.WARNING
                ConfirmationState.rejected, ConfirmationState.timed_out -> Tone.DANGER
            },
            confirmed = confirmed,
            awaitingDevice = !confirmed,
            updatedAt = value.updatedAt ?: "--",
        )
    }

    /**
     * Describes an enqueued command.
     *
     * The request body of `CommandAccepted` is always `pending`, which means the
     * broker published the command and the device has not answered yet. Callers
     * must not render this as a device confirmation; poll [commandStatus] for
     * the terminal outcome.
     */
    fun commandAccepted(value: CommandAccepted): CommandStatusView = CommandStatusView(
        requestId = value.requestId,
        state = value.status,
        stateText = "等待设备确认",
        tone = Tone.WARNING,
        settled = false,
        confirmed = false,
        failed = false,
        versionText = value.desiredVersion?.toString() ?: "--",
        errorText = "--",
    )

    /** Maps a backend command lifecycle state to host-ready semantics. */
    fun commandStatus(value: CommandStatus): CommandStatusView {
        val settled = when (value.state) {
            CommandState.accepted, CommandState.published -> false
            else -> true
        }
        val confirmed = value.state == CommandState.applied
        val failed = settled && !confirmed && value.state != CommandState.duplicate
        return CommandStatusView(
            requestId = value.requestId,
            state = value.state.name,
            stateText = when (value.state) {
                CommandState.accepted -> "命令已接受"
                CommandState.published -> "已下发，等待设备确认"
                CommandState.applied -> "设备已确认"
                CommandState.rejected -> "设备已拒绝"
                CommandState.expired -> "命令已过期"
                CommandState.duplicate -> "重复命令，已忽略"
                CommandState.failed -> "设备执行失败"
                CommandState.timed_out -> "设备确认超时"
                CommandState.publish_failed -> "下发失败"
            },
            tone = when {
                confirmed -> Tone.MINT
                !settled -> Tone.WARNING
                value.state == CommandState.duplicate -> Tone.INFO
                else -> Tone.DANGER
            },
            settled = settled,
            confirmed = confirmed,
            failed = failed,
            versionText = value.confirmedVersion?.toString() ?: "--",
            errorText = value.errorCode ?: "--",
        )
    }

    /**
     * Validates an outgoing threshold update against the contract ranges.
     *
     * @throws IllegalArgumentException when a value is outside the device-supported range.
     */
    fun validate(update: ThresholdUpdate) {
        require(update.temperatureHighC in ThresholdLimits.TEMPERATURE_MIN_C..ThresholdLimits.TEMPERATURE_MAX_C) {
            "温度阈值需在 0-80 °C"
        }
        require(update.humidityHighRh in ThresholdLimits.HUMIDITY_MIN_RH..ThresholdLimits.HUMIDITY_MAX_RH) {
            "湿度阈值需在 0-100 %RH"
        }
        require(update.gasHighPpm in ThresholdLimits.GAS_MIN_PPM..ThresholdLimits.GAS_MAX_PPM) {
            "气体阈值需在 1-999 ppm"
        }
    }

    private data class Risk(val level: String, val tone: String, val text: String, val detail: String)

    /**
     * Extracts `HH:mm:ss` from an RFC 3339 timestamp for a compact list row.
     *
     * A value that carries no time part is returned as-is rather than guessed at,
     * so an unexpected format stays visible instead of turning into a wrong time.
     */
    private fun clockText(timestamp: String): String {
        val time = timestamp.substringAfter('T', missingDelimiterValue = "")
        return if (time.isEmpty()) timestamp else time.take(8)
    }

    private fun summarize(values: List<Double>): MetricSummary {
        if (values.isEmpty()) return MetricSummary("--", "--", "--")
        return MetricSummary(decimal(values.min()), decimal(values.average()), decimal(values.max()))
    }

    /** Scales a reading onto its alarm boundary, clamped to the 0-100 bar range. */
    private fun percent(value: Double?, maximum: Double): Int {
        if (value == null || !value.isFinite()) return 0
        return ((value / maximum) * 100.0).roundToInt().coerceIn(0, 100)
    }

    /** One decimal place, trailing `.0` dropped; non-finite input renders as an empty cell. */
    private fun decimal(value: Double): String {
        if (!value.isFinite()) return "--"
        val rounded = round(value * 10.0) / 10.0
        return if (rounded == rounded.toLong().toDouble()) rounded.toLong().toString() else rounded.toString()
    }

    private fun Double.roundToInt(): Int = round(this).toInt()
}
