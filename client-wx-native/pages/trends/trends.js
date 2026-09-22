/**
 * 历史趋势页（真实折线图与统计摘要版）。
 *
 * 数据源：GET /api/v1/devices/{id}/telemetry（from/to/limit/cursor/order 契约参数）。
 * 核心特性：
 * 1. 独立 Y 轴缩放：温度 (°C, #FB7185)、湿度 (%RH, #7DD3FC)、气体 (ppm, #3FE8C3)
 *    各自独立计算极值并保留 10% 上下内边距（padding）。
 * 2. 气体缺失分段：当 gasPpm 为 null 时打断连续线段，绝不显示为 0；有效 0.0 正常显示。
 * 3. 时间比例 X 轴（all-or-nothing）：仅当全部时间戳有效且跨度为正时按时间比例分布；
 *    单样本、任一时间戳异常或全部相同则整条序列均匀分布。
 * 4. 网络防竞态：自增 requestId 避免旧请求覆盖新请求。
 * 5. 绘制防竞态：renderChart 绑定绘制版本，selector 异步回调内再次校验
 *    「页面未卸载 / 版本仍最新 / 时间窗未变」，过期回调不得清空或重绘 Canvas，
 *    也不得更新 chartRanges / axisTimes。
 * 6. 失败语义：网络失败 ≠ 空数据。失败时清空当前曲线并展示可重试的错误态，
 *    不把上一时间窗的旧曲线伪装成新时间窗；空数据与错误是两种不同状态。
 */
const deviceService = require('../../services/device.js')
const { formatRfc3339 } = require('../../utils/helpers.js')
const {
  buildChartGeometry,
  drawTrendChart,
  createRenderGate,
} = require('../../utils/trend-chart.js')

/** 时间范围选项（契约建议单次查询不超过 31 天） */
const RANGES = [
  { label: '近1小时', hours: 1 },
  { label: '近6小时', hours: 6 },
  { label: '近24小时', hours: 24 },
]

/** 网络失败时的统一提示语；与空数据文案严格区分。 */
const LOAD_ERROR_TEXT = '历史数据加载失败，请重试'

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
    /** 网络失败时的错误文案；空字符串表示无错误。与「暂无历史数据」严格区分。 */
    error: '',
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
  // 绘制门闩：网络 requestId 管请求竞态，renderGate 管 selector 回调竞态。
  renderGate: null,

  onLoad() {
    this.renderGate = createRenderGate()
  },

  onShow() {
    if (typeof this.getTabBar === 'function' && this.getTabBar()) {
      this.getTabBar().setData({ selected: 1 })
    }
    this.fetch()
  },

  onUnload() {
    this._unloaded = true
    // 递增网络版本，作废所有在途请求。
    this.requestId += 1
    // 递增绘制版本并标记 disposed，作废所有在途 selector 回调。
    if (this.renderGate) this.renderGate.dispose()
    this.chartPoints = []
    this.renderGate = null
  },

  async onRangeTap(e) {
    const index = Number(e.currentTarget.dataset.index)
    if (index === this.data.activeIndex) return
    // 切窗时立刻清空旧曲线，避免旧时间窗的曲线被误当成新时间窗展示。
    this.setData({
      activeIndex: index,
      loading: true,
      error: '',
      hasChartData: false,
      sampleCount: 0,
      stats: null,
      extremes: null,
      chartRanges: { temp: '--', hum: '--', gas: '--' },
      axisTimes: { start: '--', end: '--' },
    })
    this.chartPoints = []
    this.fetch()
  },

  /** 点击错误提示后重试当前时间窗。 */
  async onRetryTap() {
    this.fetch()
  },

  /** 拉取历史遥测并计算三项指标统计与折线图几何 */
  async fetch() {
    const reqId = ++this.requestId
    const windowKey = String(this.data.activeIndex)
    if (this.renderGate) {
      // 开启新的绘制意图；旧的 selector 回调会因版本过期而被拒绝。
      this.renderGate.nextVersion(windowKey)
    }
    this.setData({ loading: true, error: '' })
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
      if (this._unloaded || reqId !== this.requestId) return

      const rawItems = res.items || []
      // 倒序请求后在客户端反转为升序，供统计与折线图绘制
      const items = rawItems.slice().reverse()
      this.chartPoints = items

      // 空成功：曲线数据为空 + 无错误，与网络失败严格区分。
      this.setData({
        loading: false,
        error: '',
        sampleCount: items.length,
        hasChartData: items.length > 0,
        stats: items.length
          ? {
              temp: summarize(items.map((i) => i.temperatureC)),
              hum: summarize(items.map((i) => i.humidityRh)),
              gas: summarize(items.map((i) => i.gasPpm)),
            }
          : null,
        extremes: items.length
          ? {
              temp: findExtremes(items, 'temperatureC'),
              hum: findExtremes(items, 'humidityRh'),
              gas: findExtremes(items, 'gasPpm'),
            }
          : null,
      }, () => {
        this.renderChart()
      })
    } catch (e) {
      if (this._unloaded || reqId !== this.requestId) return
      // 失败策略：清空当前曲线，展示明确错误态，不把旧曲线伪装成新时间窗。
      this.chartPoints = []
      this.setData({
        loading: false,
        error: LOAD_ERROR_TEXT,
        sampleCount: 0,
        hasChartData: false,
        stats: null,
        extremes: null,
        chartRanges: { temp: '--', hum: '--', gas: '--' },
        axisTimes: { start: '--', end: '--' },
      })
    }
  },

  /**
   * 在 Canvas 2D 上绘制折线图。
   *
   * selector 回调是网络之外的第二个异步边界：回调触发时本次请求可能已过期
   * （页面卸载、更新的请求已发出、或时间窗已切换）。过期回调不得清空/重绘
   * Canvas，也不得更新 chartRanges / axisTimes。
   */
  renderChart() {
    if (this._unloaded || !this.renderGate) return
    const gate = this.renderGate
    // 记录发起绘制时的意图；回调内用 isStillWanted 重新校验。
    const drawVersion = gate.nextVersion(String(this.data.activeIndex))
    const drawWindowKey = String(this.data.activeIndex)
    const pointsSnapshot = (this.chartPoints || []).slice()

    wx.createSelectorQuery()
      .in(this)
      .select('#trendsCanvas')
      .fields({ node: true, size: true })
      .exec((res) => {
        // 异步边界重入：页面已卸载、版本已过期、或时间窗已变 → 直接放弃。
        if (!gate.isStillWanted(drawVersion, drawWindowKey)) return
        if (this._unloaded) return
        if (!res || !res[0] || !res[0].node) return
        const canvas = res[0].node
        const ctx = canvas.getContext('2d')
        const dpr = (wx.getSystemInfoSync && wx.getSystemInfoSync().pixelRatio) || 1
        const width = res[0].width || 300
        const height = res[0].height || 180

        canvas.width = width * dpr
        canvas.height = height * dpr
        ctx.scale(dpr, dpr)

        const geometry = buildChartGeometry(pointsSnapshot, {
          width,
          height,
          padding: { left: 16, right: 16, top: 16, bottom: 16 },
        })

        drawTrendChart(ctx, geometry)

        // setData 前再校验一次：绘制过程中状态可能已变化。
        if (!gate.isStillWanted(drawVersion, drawWindowKey) || this._unloaded) return

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
