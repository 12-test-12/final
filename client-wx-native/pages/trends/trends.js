/**
 * 历史趋势页（真实折线图与统计摘要版）。
 *
 * 数据源：GET /api/v1/devices/{id}/telemetry（from/to/limit/cursor/order 契约参数）。
 * 核心特性：
 * 1. 独立 Y 轴缩放：温度 (°C, #FB7185)、湿度 (%RH, #7DD3FC)、气体 (ppm, #3FE8C3)
 *    各自独立计算极值并保留 10% 上下内边距（padding）。
 * 2. 气体缺失分段：当 gasPpm 为 null 时打断连续线段，绝不显示为 0；有效 0.0 正常显示。
 * 3. 时间比例 X 轴：根据样本时间戳相对首尾跨度线性分布；单样本或时间戳相同时均匀分布。
 * 4. 防竞态保护：自增 requestId 避免旧请求覆盖新请求。
 */
const deviceService = require('../../services/device.js')
const { formatRfc3339 } = require('../../utils/helpers.js')
const { buildChartGeometry, drawTrendChart } = require('../../utils/trend-chart.js')

/** 时间范围选项（契约建议单次查询不超过 31 天） */
const RANGES = [
  { label: '近1小时', hours: 1 },
  { label: '近6小时', hours: 6 },
  { label: '近24小时', hours: 24 },
]

/** 汇总统计：最低/平均/最高，保留 1 位小数。排除 null/undefined/NaN，防止 Math.min(null) 误转为 0 */
function summarize(values) {
  const valid = values.filter((v) => v != null && !isNaN(v) && isFinite(v))
  if (!valid.length) return { min: '--', max: '--', avg: '--' }
  const min = Math.min.apply(null, valid)
  const max = Math.max.apply(null, valid)
  const avg = valid.reduce((s, v) => s + v, 0) / valid.length
  const f = (v) => v.toFixed(1)
  return { min: f(min), max: f(max), avg: f(avg) }
}

/**
 * 极值时刻：从真实历史样本里找出最大/最小值的发生时间。
 * 注意：排除无效值，不由当前值反推，只使用返回的样本点。
 */
function findExtremes(items, key) {
  const validItems = items.filter((it) => it[key] != null && !isNaN(it[key]) && isFinite(it[key]))
  if (!validItems.length) return { maxAt: '--', minAt: '--' }
  let maxItem = validItems[0]
  let minItem = validItems[0]
  validItems.forEach((it) => {
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
    hasChartData: false,
    chartRanges: {
      temp: '--',
      hum: '--',
      gas: '--',
    },
    axisTimes: {
      start: '--',
      end: '--',
    },
  },

  requestId: 0,
  chartPoints: [],

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

  /** 拉取历史遥测并计算三项指标统计与折线图几何 */
  async fetch() {
    const reqId = ++this.requestId
    this.setData({ loading: true })
    try {
      const hours = RANGES[this.data.activeIndex].hours
      const to = new Date().toISOString()
      const from = new Date(Date.now() - hours * 3600 * 1000).toISOString()
      const res = await deviceService.getTelemetryHistory('MCU001', {
        from,
        to,
        limit: 200,
        order: 'desc',
      })
      if (reqId !== this.requestId) return

      const rawItems = res.items || []
      // 倒序请求后在客户端反转为升序，供统计与折线图绘制
      const items = rawItems.slice().reverse()
      this.chartPoints = items

      this.setData({
        loading: false,
        sampleCount: items.length,
        hasChartData: items.length > 0,
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
      }, () => {
        this.renderChart()
      })
    } catch (e) {
      if (reqId !== this.requestId) return
      this.setData({ loading: false })
      wx.showToast({ title: (e && e.message) || '加载失败', icon: 'none' })
    }
  },

  /** 在 Canvas 2D 上绘制折线图 */
  renderChart() {
    wx.createSelectorQuery()
      .in(this)
      .select('#trendsCanvas')
      .fields({ node: true, size: true })
      .exec((res) => {
        if (!res || !res[0] || !res[0].node) return
        const canvas = res[0].node
        const ctx = canvas.getContext('2d')
        const dpr = (wx.getSystemInfoSync && wx.getSystemInfoSync().pixelRatio) || 1
        const width = res[0].width || 300
        const height = res[0].height || 180

        canvas.width = width * dpr
        canvas.height = height * dpr
        ctx.scale(dpr, dpr)

        const geometry = buildChartGeometry(this.chartPoints || [], {
          width,
          height,
          padding: { left: 16, right: 16, top: 16, bottom: 16 },
        })

        drawTrendChart(ctx, geometry)

        this.setData({
          chartRanges: {
            temp: geometry.tempRangeText,
            hum: geometry.humRangeText,
            gas: geometry.gasRangeText,
          },
          axisTimes: {
            start: geometry.xStartText,
            end: geometry.xEndText,
          },
        })
      })
  },
})
