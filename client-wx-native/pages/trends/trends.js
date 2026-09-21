/**
 * 历史趋势页（当前为统计摘要版）。
 *
 * 数据源：GET /api/v1/devices/{id}/telemetry（from/to/limit/cursor 契约参数）。
 * 下一步接入 ECharts（ec-canvas）渲染三指标折线图后，
 * 本页统计卡片保留，图表占位区替换为真实图表组件。
 */
const deviceService = require('../../services/device.js')
const { formatRfc3339 } = require('../../utils/helpers.js')

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

/**
 * 极值时刻：从真实历史样本里找出最大/最小值的发生时间。
 * 注意：不由当前值反推，只使用返回的样本点。
 */
function findExtremes(items, key) {
  if (!items.length) return { maxAt: '--', minAt: '--' }
  let maxItem = items[0]
  let minItem = items[0]
  items.forEach((it) => {
    if (it[key] > maxItem[key]) maxItem = it
    if (it[key] < minItem[key]) minItem = it
  })
  return {
    maxAt: formatRfc3339(maxItem.timestamp || maxItem.receivedAt),
    minAt: formatRfc3339(minItem.timestamp || minItem.receivedAt),
  }
}

Page({
  data: {
    ranges: RANGES.map((r) => r.label),
    activeIndex: 0,
    loading: true,
    stats: null,
    sampleCount: 0,
    extremes: null,
  },

  onShow() {
    if (typeof this.getTabBar === 'function' && this.getTabBar()) {
      this.getTabBar().setData({ selected: 1 })
    }
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
        sampleCount: items.length,
        stats: {
          temp: summarize(items.map((i) => i.temperatureC)),
          hum: summarize(items.map((i) => i.humidityRh)),
          gas: summarize(items.map((i) => i.gasPpm)),
        },
        extremes: {
          temp: findExtremes(items, 'temperatureC'),
          hum: findExtremes(items, 'humidityRh'),
          gas: findExtremes(items, 'gasPpm'),
        },
      })
    } catch (e) {
      this.setData({ loading: false })
      wx.showToast({ title: (e && e.message) || '加载失败', icon: 'none' })
    }
  },
})
