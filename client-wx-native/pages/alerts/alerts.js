/**
 * 告警记录页。
 *
 * 数据源：GET /api/v1/devices/{id}/alerts（支持 state/active 过滤，本页先展示全量）。
 * 注意：告警触发证据（evidence）由后端保存，页面不得用最新值反推历史告警原因。
 */
const deviceService = require('../../services/device.js')
const socket = require('../../services/socket.js')
const { formatRfc3339 } = require('../../utils/helpers.js')

/** 告警状态 -> 标签样式与文案（枚举值来自契约 §7） */
const STATE_META = {
  suspect: { tag: 'tag-amber', text: '疑似异常' },
  fire_warning: { tag: 'tag-red', text: '火情预警' },
  acknowledged: { tag: 'tag-amber', text: '已确认' },
  recovered: { tag: 'tag-green', text: '已恢复' },
}

/** 筛选标签：all 表示不过滤 */
const FILTERS = [
  { key: 'all', label: '全部' },
  { key: 'fire_warning', label: '火情' },
  { key: 'acknowledged', label: '已确认' },
  { key: 'recovered', label: '已恢复' },
]

Page({
  data: {
    loading: true,
    error: '',
    alerts: [],
    shown: [],
    filters: FILTERS,
    activeFilter: 'all',
  },

  onShow() {
    if (typeof this.getTabBar === 'function' && this.getTabBar()) {
      this.getTabBar().setData({ selected: 2 })
    }
    this.fetch()
    this.subscribeStream()
  },

  onHide() {
    this.unsubscribeStream()
  },

  onUnload() {
    this.unsubscribeStream()
  },

  /** 订阅复合预警状态变化：新事件产生时自动刷新列表（去抖，避免连续事件重复请求） */
  subscribeStream() {
    if (this._offs) return
    socket.connect('MCU001', { onResync: () => this.fetch() })
    this._offs = [
      socket.on('alert.state_changed', () => {
        if (this._refreshTimer) clearTimeout(this._refreshTimer)
        this._refreshTimer = setTimeout(() => this.fetch(), 800)
      }),
    ]
  },

  unsubscribeStream() {
    if (this._offs) {
      this._offs.forEach((off) => off())
      this._offs = null
    }
    if (this._refreshTimer) {
      clearTimeout(this._refreshTimer)
      this._refreshTimer = null
    }
  },

  async fetch() {
    this.setData({ loading: true })
    try {
      const res = await deviceService.getAlerts('MCU001', { limit: 50 })
      const alerts = (res.items || []).map((a) => ({
        id: a.id,
        state: a.state,
        startedAt: a.startedAt,
        acknowledgedAt: a.acknowledgedAt,
        endedAt: a.endedAt,
        evidence: a.evidence,
        stateMeta: STATE_META[a.state] || { tag: 'tag-gray', text: a.state },
        startedText: formatRfc3339(a.startedAt),
        ackText: a.acknowledgedAt ? formatRfc3339(a.acknowledgedAt) : '',
        endedText: a.endedAt ? formatRfc3339(a.endedAt) : '',
      }))
      this.setData({ alerts, loading: false, error: '' })
      this.applyFilter(this.data.activeFilter, alerts)
    } catch (e) {
      this.setData({ loading: false, error: (e && e.message) || '加载失败' })
    }
  },

  /** 本地筛选，避免为了切换标签重复请求后端 */
  applyFilter(key, list) {
    const source = list || this.data.alerts
    this.setData({
      activeFilter: key,
      shown: key === 'all' ? source : source.filter((a) => a.state === key),
    })
  },

  onFilterTap(e) {
    const key = e.currentTarget.dataset.key
    if (key === this.data.activeFilter) return
    this.applyFilter(key)
  },
})
