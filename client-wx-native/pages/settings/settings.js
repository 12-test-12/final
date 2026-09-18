/**
 * 阈值设置页。
 *
 * 数据源：GET/PUT /api/v1/devices/{id}/thresholds。
 * 契约要点：
 * - temperatureHighC 范围 0-80 °C，gasHighPpm 范围 1-999 ppm；
 * - PUT 返回 202 只表示后端接受命令，设备确认后 confirmationState 才变 confirmed；
 * - 期望版本(desiredVersion)与设备确认版本(confirmedVersion)不一致说明命令还在途中。
 */
const deviceService = require('../../services/device.js')
const { formatRfc3339 } = require('../../utils/helpers.js')

/** 设备确认状态文案（契约 §8：confirmed | pending | rejected | timed_out） */
const CONFIRM_TEXT = {
  confirmed: '设备已确认',
  pending: '等待设备确认',
  rejected: '设备已拒绝',
  timed_out: '确认超时，请重试',
}

Page({
  data: {
    loading: true,
    temperatureHighC: 30,
    gasHighPpm: 80,
    desiredVersion: 0,
    confirmedVersion: 0,
    confirmationState: 'confirmed',
    confirmText: '',
    updatedAt: '',
    saving: false,
  },

  onShow() {
    this.fetch()
  },

  /** 拉取当前阈值与版本信息 */
  async fetch() {
    try {
      const t = await deviceService.getThresholds('MCU001')
      this.setData({
        loading: false,
        temperatureHighC: t.temperatureHighC,
        gasHighPpm: t.gasHighPpm,
        desiredVersion: t.desiredVersion,
        confirmedVersion: t.confirmedVersion,
        confirmationState: t.confirmationState,
        confirmText: CONFIRM_TEXT[t.confirmationState] || t.confirmationState,
        updatedAt: formatRfc3339(t.updatedAt),
      })
    } catch (e) {
      this.setData({ loading: false })
      wx.showToast({ title: (e && e.message) || '加载失败', icon: 'none' })
    }
  },

  onTempChange(e) {
    this.setData({ temperatureHighC: e.detail.value })
  },

  onGasChange(e) {
    this.setData({ gasHighPpm: e.detail.value })
  },

  /**
   * 保存并下发。客户端先做范围校验减少无效请求，
   * 服务端仍会以 422 invalid_threshold 兜底。
   */
  async onSave() {
    if (this.data.saving) return
    const { temperatureHighC, gasHighPpm } = this.data
    if (temperatureHighC < 0 || temperatureHighC > 80) {
      wx.showToast({ title: '温度阈值需在 0-80 °C', icon: 'none' })
      return
    }
    if (gasHighPpm < 1 || gasHighPpm > 999) {
      wx.showToast({ title: '气体阈值需在 1-999 ppm', icon: 'none' })
      return
    }
    this.setData({ saving: true })
    try {
      const res = await deviceService.putThresholds('MCU001', {
        temperatureHighC: Number(temperatureHighC),
        gasHighPpm: Number(gasHighPpm),
      })
      this.setData({
        desiredVersion: res.desiredVersion,
        confirmationState: 'pending',
        confirmText: CONFIRM_TEXT.pending,
      })
      wx.showToast({ title: '已下发，等待设备确认', icon: 'none' })
      // 模拟设备确认后的刷新；真实后端接入后由 thresholds.confirmed 事件驱动
      setTimeout(() => this.fetch(), 3500)
    } catch (e) {
      wx.showToast({ title: (e && e.message) || '下发失败', icon: 'none' })
    } finally {
      this.setData({ saving: false })
    }
  },
})
