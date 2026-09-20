/**
 * 历史趋势页（当前为统计摘要版）。
 *
 * 数据源：GET /api/v1/devices/{id}/telemetry（from/to/limit/cursor 契约参数）。
 * 下一步接入 ECharts（ec-canvas）渲染三指标折线图后，
 * 本页统计卡片保留，图表占位区替换为真实图表组件。
 */
const deviceService = require('../../services/device.js')

/** 时间范围选项（契约建议单次查询不超过 31 天） */
const RANGES = [
  { label: '近1小时', hours: 1 },
  { label: '近6小时', hours: 6 },
  { label: '近24小时', hours: 24 },
]

/** 汇总统计：最低/平均/最高，保留 1 位小数 */
function summarize(values) {
  if (!values.length) return { min: '--', max: '--', avg: '--' }
  const min = Math.min.apply(null, values)
  const max = Math.max.apply(null, values)
  const avg = values.reduce((s, v) => s + v, 0) / values.length
  const f = (v) => v.toFixed(1)
  return { min: f(min), max: f(max), avg: f(avg) }
}

Page({
  data: {
    ranges: RANGES.map((r) => r.label),
    activeIndex: 0,
    loading: true,
    stats: null,
  },

  onShow() {
    this.fetch()
  },

  async onRangeTap(e) {
    const index = Number(e.currentTarget.dataset.index)
    if (index === this.data.activeIndex) return
    this.setData({ activeIndex: index })
    this.fetch()
  },

  /** 拉取历史遥测并计算三项指标统计 */
  async fetch() {
    this.setData({ loading: true })
    try {
      const hours = RANGES[this.data.activeIndex].hours
      const from = new Date(Date.now() - hours * 3600 * 1000).toISOString()
      const res = await deviceService.getTelemetryHistory('MCU001', { from, limit: 60 })
      const items = res.items || []
      this.setData({
        loading: false,
        stats: {
          temp: summarize(items.map((i) => i.temperatureC)),
          hum: summarize(items.map((i) => i.humidityRh)),
          gas: summarize(items.map((i) => i.gasPpm)),
        },
      })
    } catch (e) {
      this.setData({ loading: false })
      wx.showToast({ title: (e && e.message) || '加载失败', icon: 'none' })
    }
  },
})
