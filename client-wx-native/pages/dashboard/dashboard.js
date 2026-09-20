/**
 * 实时监控首页。
 *
 * 数据流：REST status + latest 完成初始化，随后轮询 latest 刷新。
 * Mock 阶段用轮询模拟实时推送；接入真实后端后改为 WebSocket
 * （/ws/v1/devices/{id}/telemetry）订阅，重连后先 REST 补数再订阅。
 */
const deviceService = require('../../services/device.js')
const { formatTime } = require('../../utils/helpers.js')

/** 复合预警状态 -> 横幅文案与配色（枚举值来自契约 §4） */
const ALARM_STATE = {
  normal: { level: 'normal', text: '环境正常', sub: '各项指标处于安全范围' },
  suspect: { level: 'suspect', text: '疑似异常', sub: '部分条件异常，系统确认中' },
  fire_warning: { level: 'fire', text: '火情预警', sub: '气体突增与温升速率同时超限' },
  acknowledged: { level: 'suspect', text: '告警已确认', sub: '人员已确认，等待环境恢复' },
  recovered: { level: 'recovered', text: '指标已恢复', sub: '事件归档中' },
}

/** 设备连接状态 -> 标签配色 */
const CONNECTIVITY = {
  online: { level: 'green', text: '在线' },
  offline: { level: 'red', text: '离线' },
  unknown: { level: 'gray', text: '未知' },
}

Page({
  data: {
    deviceId: 'MCU001',
    loading: true,
    error: '',
    banner: ALARM_STATE.normal,
    connectivity: CONNECTIVITY.unknown,
    latest: null,
    view: null,
    updatedAt: '--:--:--',
    muting: false,
  },

  onLoad() {
    this.fetchInitial()
  },
  onShow() {
    this.startPolling()
  },
  onHide() {
    this.stopPolling()
  },
  onUnload() {
    this.stopPolling()
  },

  /** 初始化：并行拉取设备状态与最新遥测 */
  async fetchInitial() {
    try {
      const [status, latest] = await Promise.all([
        deviceService.getStatus(this.data.deviceId),
        deviceService.getLatestTelemetry(this.data.deviceId),
      ])
      this.applyStatus(status)
      this.applyTelemetry(latest)
      this.setData({ loading: false, error: '' })
    } catch (e) {
      this.setData({ loading: false, error: (e && e.message) || '数据加载失败' })
    }
  },

  applyStatus(status) {
    this.setData({
      banner: ALARM_STATE[status.alarmState] || ALARM_STATE.normal,
      connectivity: CONNECTIVITY[status.connectivity] || CONNECTIVITY.unknown,
    })
  },

  /** 将遥测转换为视图模型：字符串数值 + 进度条百分比 */
  applyTelemetry(latest) {
    this.setData({
      latest,
      updatedAt: formatTime(latest.receivedAt || latest.timestamp),
      view: {
        temperatureC: latest.temperatureC.toFixed(1),
        humidityRh: latest.humidityRh.toFixed(1),
        gasPpm: latest.gasPpm.toFixed(1),
        tempPercent: Math.min(100, (latest.temperatureC / 40) * 100),
        humPercent: Math.min(100, latest.humidityRh),
        gasPercent: Math.min(100, (latest.gasPpm / 100) * 100),
      },
    })
  },

  startPolling() {
    if (this._timer) return
    this._timer = setInterval(async () => {
      try {
        const latest = await deviceService.getLatestTelemetry(this.data.deviceId)
        this.applyTelemetry(latest)
      } catch (e) {
        // 单次刷新失败不打断页面，等待下一轮
      }
    }, 2000)
  },

  stopPolling() {
    if (this._timer) {
      clearInterval(this._timer)
      this._timer = null
    }
  },

  /** 远程静音/恢复：202 表示命令已被后端接受，等待设备确认 */
  async onMuteTap() {
    if (this.data.muting || !this.data.latest) return
    const muted = !this.data.latest.buzzerMuted
    this.setData({ muting: true })
    try {
      await deviceService.muteBuzzer(this.data.deviceId, muted)
      this.setData({ 'latest.buzzerMuted': muted })
      wx.showToast({ title: muted ? '静音命令已下发' : '恢复命令已下发', icon: 'none' })
    } catch (e) {
      wx.showToast({ title: (e && e.message) || '命令下发失败', icon: 'none' })
    } finally {
      this.setData({ muting: false })
    }
  },
})
