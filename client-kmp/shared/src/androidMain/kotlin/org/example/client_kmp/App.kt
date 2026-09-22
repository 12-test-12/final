package org.example.client_kmp

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.example.client_kmp.monitoring.AlertItemView
import org.example.client_kmp.monitoring.AlertsView
import org.example.client_kmp.monitoring.DashboardView
import org.example.client_kmp.monitoring.MonitoringClient
import org.example.client_kmp.monitoring.SettingsView
import org.example.client_kmp.monitoring.ThresholdLimits
import org.example.client_kmp.monitoring.ThresholdUpdate
import org.example.client_kmp.monitoring.Tone
import org.example.client_kmp.monitoring.TrendPointView
import org.example.client_kmp.monitoring.TrendsView

private val Background = Color(0xFF071310)
private val Surface = Color(0xFF10231E)
private val SurfaceLight = Color(0xFF17302A)
private val Mint = Color(0xFF3FE8C3)
private val TextPrimary = Color(0xFFECF5F2)
private val TextSecondary = Color(0xFF8FA8A2)
private val Danger = Color(0xFFFB7185)
private val Warning = Color(0xFFF5C96B)
private val Info = Color(0xFF7DD3FC)

/** Backend address reachable from the Android emulator; the host is `10.0.2.2`. */
private const val EMULATOR_BASE_URL = "http://10.0.2.2:8080"

private const val DASHBOARD_REFRESH_MS = 3_000L

private enum class MonitorTab(val title: String, val glyph: String) {
    Dashboard("监控", "◉"), Trends("趋势", "⌁"), Alerts("告警", "!"), Settings("设置", "⚙")
}

/**
 * Android host UI.
 *
 * Every value, label and colour token comes from the shared
 * [MonitoringClient]; this file only arranges them. The client is remembered so
 * recomposition never builds a second one, and each screen's polling lives in a
 * `LaunchedEffect`, so switching tabs or destroying the Activity cancels it
 * instead of leaking a coroutine.
 */
@Composable
fun App() {
    val client = remember { MonitoringClient(AndroidMonitoringPlatform(), EMULATOR_BASE_URL) }
    var tab by remember { mutableStateOf(MonitorTab.Dashboard) }
    MaterialTheme {
        Scaffold(
            containerColor = Background,
            bottomBar = {
                NavigationBar(containerColor = Color(0xFF0D1E1A)) {
                    MonitorTab.entries.forEach { item ->
                        NavigationBarItem(
                            selected = tab == item,
                            onClick = { tab = item },
                            icon = { Text(item.glyph, color = if (tab == item) Mint else TextSecondary) },
                            label = { Text(item.title, color = if (tab == item) Mint else TextSecondary) },
                        )
                    }
                }
            },
        ) { padding ->
            Box(Modifier.fillMaxSize().padding(padding).background(Background)) {
                when (tab) {
                    MonitorTab.Dashboard -> DashboardScreen(client)
                    MonitorTab.Trends -> TrendsScreen(client)
                    MonitorTab.Alerts -> AlertsScreen(client)
                    MonitorTab.Settings -> SettingsScreen(client)
                }
            }
        }
    }
}

@Composable
private fun Page(title: String, subtitle: String, content: @Composable ColumnScope.() -> Unit) {
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 18.dp, vertical = 20.dp),
    ) {
        Text(title, color = TextPrimary, fontSize = 28.sp, fontWeight = FontWeight.SemiBold)
        Text(subtitle, color = TextSecondary, fontSize = 13.sp, modifier = Modifier.padding(top = 4.dp, bottom = 18.dp))
        content()
        Spacer(Modifier.height(24.dp))
    }
}

@Composable
private fun GlassCard(content: @Composable ColumnScope.() -> Unit) {
    Column(
        Modifier.fillMaxWidth().padding(vertical = 7.dp)
            .background(Brush.linearGradient(listOf(SurfaceLight, Surface)), RoundedCornerShape(20.dp))
            .padding(18.dp),
        content = content,
    )
}

@Composable
private fun SectionTitle(title: String, tip: String = "") {
    Row(Modifier.fillMaxWidth().padding(top = 18.dp, bottom = 7.dp), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(title, color = TextPrimary, fontWeight = FontWeight.SemiBold, fontSize = 19.sp)
        Text(tip, color = TextSecondary, fontSize = 12.sp)
    }
}

@Composable
private fun Hint(text: String, color: Color = TextSecondary) {
    Text(text, color = color, fontSize = 13.sp, modifier = Modifier.padding(vertical = 12.dp))
}

@Composable
private fun DashboardScreen(client: MonitoringClient) {
    var view by remember { mutableStateOf<DashboardView?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var commandHint by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()

    // Refreshes once per interval. Failures are captured into `error`, so a
    // network blip can never terminate the loop: the next tick retries.
    LaunchedEffect(Unit) {
        while (true) {
            runCatching { client.loadDashboard() }
                .onSuccess { view = it; error = null }
                .onFailure { error = it.message }
            delay(DASHBOARD_REFRESH_MS)
        }
    }

    // Disabled while in flight, so a double tap cannot enqueue two commands.
    fun toggleMute(currentlyMuted: Boolean) {
        if (busy) return
        busy = true
        scope.launch {
            runCatching {
                val accepted = client.setMuted(!currentlyMuted)
                commandHint = accepted.stateText
                // The enqueue response is only an acknowledgement; wait for the device.
                val settled = client.awaitCommandOutcome(accepted.requestId)
                commandHint = settled?.stateText ?: "等待设备确认"
            }.onFailure { error = it.message }
            runCatching { client.loadDashboard() }.onSuccess { view = it }
            busy = false
        }
    }

    Page("机房环境总览", "智慧机房 · 实时动环监测") {
        if (view == null && error == null) Hint("数据加载中…")
        error?.let { Hint(it, Danger) }
        view?.let { data ->
            GlassCard {
                Text("系统风险状态", color = TextSecondary, fontSize = 13.sp)
                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(top = 8.dp)) {
                    Box(Modifier.size(10.dp).background(toneColor(data.riskTone), CircleShape))
                    Text(
                        data.riskText,
                        color = toneColor(data.riskTone),
                        fontSize = 25.sp,
                        fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.padding(start = 10.dp),
                    )
                    Spacer(Modifier.weight(1f))
                    Pill(data.connectivityText, if (data.online) Mint else TextSecondary)
                }
                Text(data.riskDetail, color = TextSecondary, fontSize = 13.sp, modifier = Modifier.padding(top = 6.dp))
            }
            if (!data.hasData) Hint("该设备尚未上报有效遥测数据", Warning)
            SectionTitle("实时数据", "每 3 秒同步")
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Meter("T", "温度", data.temperatureText, "°C", data.temperaturePercent, Color(0xFFFFA183), Modifier.weight(1f))
                Meter("H", "湿度", data.humidityText, "%", data.humidityPercent, Color(0xFF8ED6FF), Modifier.weight(1f))
                Meter("G", "气体", data.gasText, "ppm", data.gasPercent, Mint, Modifier.weight(1f))
            }
            SectionTitle("设备状态", "本地采集终端")
            GlassCard {
                Text(data.deviceId, color = TextPrimary, fontSize = 22.sp, fontWeight = FontWeight.SemiBold)
                KeyValue("本地报警", data.localAlarmText, if (data.localAlarm) Danger else Mint)
                KeyValue("声光提示", data.buzzerText, if (data.buzzerMuted) Warning else TextPrimary)
                KeyValue("更新时间", data.updatedAt, TextPrimary)
            }
            SectionTitle("远程控制", "静音不影响检测与上报")
            GlassCard {
                Text("蜂鸣器控制", color = TextPrimary, fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
                Text("本地报警状态不会因静音而清除", color = TextSecondary, fontSize = 12.sp)
                if (commandHint.isNotEmpty()) Hint(commandHint, Warning)
                Button(
                    enabled = !busy,
                    onClick = { toggleMute(data.buzzerMuted) },
                    colors = ButtonDefaults.buttonColors(containerColor = Mint, contentColor = Background),
                    modifier = Modifier.fillMaxWidth().padding(top = 14.dp),
                ) { Text(if (busy) "下发中…" else if (data.buzzerMuted) "恢复鸣叫" else "远程静音") }
            }
        }
    }
}

@Composable
private fun Meter(letter: String, label: String, value: String, unit: String, percent: Int, color: Color, modifier: Modifier) {
    Column(modifier.background(Surface, RoundedCornerShape(16.dp)).padding(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(letter, color = color, fontWeight = FontWeight.Bold)
            Text(label, color = TextSecondary, fontSize = 12.sp, modifier = Modifier.padding(start = 6.dp))
        }
        Text(value, color = TextPrimary, fontSize = 25.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(top = 8.dp))
        Text(unit, color = TextSecondary, fontSize = 10.sp)
        Box(Modifier.fillMaxWidth().height(4.dp).background(Color.White.copy(alpha = .08f), CircleShape)) {
            Box(Modifier.fillMaxWidth(percent / 100f).height(4.dp).background(color, CircleShape))
        }
    }
}

@Composable
private fun TrendsScreen(client: MonitoringClient) {
    var view by remember { mutableStateOf<TrendsView?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        runCatching { client.loadTrends() }.onSuccess { view = it }.onFailure { error = it.message }
    }
    Page("历史趋势", "数据统计与曲线") {
        if (view == null && error == null) Hint("数据加载中…")
        error?.let { Hint(it, Danger) }
        view?.let { data ->
            if (!data.hasData) {
                Hint("所选区间内没有遥测样本")
            } else {
                SectionTitle("统计摘要", "共 ${data.sampleCount} 条样本")
                Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    SummaryCard("温度", "°C", data.temperature.minimum, data.temperature.average, data.temperature.maximum)
                    SummaryCard("湿度", "%RH", data.humidity.minimum, data.humidity.average, data.humidity.maximum)
                    SummaryCard(
                        "气体", "ppm",
                        if (data.gasSampleCount > 0) data.gas.minimum else "--",
                        if (data.gasSampleCount > 0) data.gas.average else "--",
                        if (data.gasSampleCount > 0) data.gas.maximum else "--",
                    )
                }
                if (data.gasSampleCount < data.sampleCount) {
                    Hint("${data.sampleCount - data.gasSampleCount} 条样本没有已校准气体读数，未计入气体统计")
                }
                SectionTitle("采样序列", "按时间升序")
                GlassCard {
                    data.series.takeLast(SERIES_PREVIEW).forEach { TrendRow(it) }
                }
            }
        }
    }
}

private const val SERIES_PREVIEW = 12

@Composable
private fun TrendRow(point: TrendPointView) {
    Row(Modifier.fillMaxWidth().padding(vertical = 5.dp), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(point.timeText, color = TextSecondary, fontSize = 12.sp)
        Text(
            "${point.temperatureText}°C   ${point.humidityText}%   ${point.gasText}ppm",
            color = if (point.localAlarm) Danger else TextSecondary,
            fontSize = 12.sp,
        )
    }
}

@Composable
private fun SummaryCard(name: String, unit: String, min: String, avg: String, max: String) {
    Column(Modifier.width(180.dp).background(Surface, RoundedCornerShape(18.dp)).padding(16.dp)) {
        Text("$name  $unit", color = TextSecondary)
        Text(avg, color = TextPrimary, fontSize = 32.sp, fontWeight = FontWeight.SemiBold)
        Text("最低 $min    最高 $max", color = TextSecondary, fontSize = 12.sp)
    }
}

@Composable
private fun AlertsScreen(client: MonitoringClient) {
    var view by remember { mutableStateOf<AlertsView?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        runCatching { client.loadAlerts() }.onSuccess { view = it }.onFailure { error = it.message }
    }
    Page("告警记录", "复合预警事件与触发证据") {
        if (view == null && error == null) Hint("数据加载中…")
        error?.let { Hint(it, Danger) }
        view?.let { data ->
            if (data.items.isEmpty()) Hint("暂无告警记录")
            data.items.forEach { AlertCard(it) }
        }
    }
}

@Composable
private fun AlertCard(item: AlertItemView) {
    GlassCard {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            Pill(item.stateText, toneColor(item.tone))
            Text(item.startedAt, color = TextSecondary, fontSize = 11.sp)
        }
        Text("触发证据", color = TextSecondary, fontSize = 12.sp, modifier = Modifier.padding(top = 14.dp))
        KeyValue("气体 ADC 上升", "${item.gasAdcRiseText}（阈值 ${item.gasAdcRiseThresholdText}）", TextPrimary)
        KeyValue("温升速率", "${item.temperatureRateText}（阈值 ${item.temperatureRateThresholdText}）", TextPrimary)
        KeyValue("样本数", item.sampleCountText, TextPrimary)
        KeyValue("窗口", "${item.windowSecondsText} 秒", TextPrimary)
        KeyValue("结束时间", item.endedAt, TextPrimary)
    }
}

@Composable
private fun SettingsScreen(client: MonitoringClient) {
    var view by remember { mutableStateOf<SettingsView?>(null) }
    var temperature by remember { mutableStateOf(30f) }
    var humidity by remember { mutableStateOf(80f) }
    var gas by remember { mutableStateOf(20f) }
    var error by remember { mutableStateOf<String?>(null) }
    var saving by remember { mutableStateOf(false) }
    var commandHint by remember { mutableStateOf("") }
    val scope = rememberCoroutineScope()

    suspend fun refresh() {
        runCatching { client.loadSettings() }.onSuccess {
            view = it
            temperature = it.temperatureHighC.toFloat()
            humidity = it.humidityHighRh.toFloat()
            gas = it.gasHighPpm.toFloat()
            error = null
        }.onFailure { error = it.message }
    }
    LaunchedEffect(Unit) { refresh() }

    fun save() {
        if (saving) return
        saving = true
        scope.launch {
            runCatching {
                // The shared client rejects an out-of-range value before any
                // network call, so the message names the contract range.
                val accepted = client.updateThresholds(
                    ThresholdUpdate(temperature.toDouble(), humidity.toDouble(), gas.toDouble()),
                )
                commandHint = accepted.stateText
                val settled = client.awaitCommandOutcome(accepted.requestId)
                commandHint = settled?.stateText ?: "等待设备确认"
            }.onFailure { error = it.message }
            refresh()
            saving = false
        }
    }

    Page("预警阈值", "设置设备本地报警的安全边界") {
        if (view == null && error == null) Hint("数据加载中…")
        error?.let { Hint(it, Danger) }
        view?.let { data ->
            // Ranges come from the shared contract constants, so a slider can
            // never offer a value the backend would reject with 422.
            ThresholdSlider("温度上限", temperature, "°C", temperatureRange()) { temperature = it }
            ThresholdSlider("湿度上限", humidity, "%RH", humidityRange()) { humidity = it }
            ThresholdSlider("气体浓度上限", gas, "ppm", gasRange()) { gas = it }
            SectionTitle("设备确认", "202 仅表示命令已接受")
            GlassCard {
                KeyValue("规则同步状态", data.confirmationText, toneColor(data.confirmationTone))
                KeyValue("期望版本", data.desiredVersion.toString(), TextPrimary)
                KeyValue("设备确认版本", data.confirmedVersion?.toString() ?: "--", TextPrimary)
                KeyValue("更新时间", data.updatedAt, TextPrimary)
            }
            if (commandHint.isNotEmpty()) Hint(commandHint, Warning)
            Button(
                enabled = !saving,
                onClick = { save() },
                colors = ButtonDefaults.buttonColors(containerColor = Mint, contentColor = Background),
                modifier = Modifier.fillMaxWidth().padding(top = 20.dp),
            ) { Text(if (saving) "正在下发…" else "保存并下发到设备") }
        }
    }
}

@Composable
private fun ThresholdSlider(label: String, value: Float, unit: String, range: ClosedFloatingPointRange<Float>, onChange: (Float) -> Unit) {
    GlassCard {
        Text(label, color = TextSecondary)
        Text("${value.toInt()} $unit", color = TextPrimary, fontSize = 34.sp, fontWeight = FontWeight.SemiBold)
        Slider(value = value, onValueChange = onChange, valueRange = range)
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            Text(range.start.toInt().toString(), color = TextSecondary, fontSize = 11.sp)
            Text(range.endInclusive.toInt().toString(), color = TextSecondary, fontSize = 11.sp)
        }
    }
}

@Composable
private fun KeyValue(label: String, value: String, color: Color) {
    Row(Modifier.fillMaxWidth().padding(top = 12.dp), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(label, color = TextSecondary, fontSize = 13.sp)
        Text(value, color = color, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
    }
}

@Composable
private fun Pill(text: String, color: Color) {
    Text(text, color = color, fontSize = 12.sp, modifier = Modifier.background(color.copy(alpha = .14f), CircleShape).padding(horizontal = 10.dp, vertical = 4.dp))
}

/** Maps a shared tone token onto this host's palette. */
private fun toneColor(tone: String): Color = when (tone) {
    Tone.DANGER -> Danger
    Tone.WARNING -> Warning
    Tone.INFO -> Info
    else -> Mint
}

private fun temperatureRange() =
    ThresholdLimits.TEMPERATURE_MIN_C.toFloat()..ThresholdLimits.TEMPERATURE_MAX_C.toFloat()

private fun humidityRange() =
    ThresholdLimits.HUMIDITY_MIN_RH.toFloat()..ThresholdLimits.HUMIDITY_MAX_RH.toFloat()

private fun gasRange() =
    ThresholdLimits.GAS_MIN_PPM.toFloat()..ThresholdLimits.GAS_MAX_PPM.toFloat()
