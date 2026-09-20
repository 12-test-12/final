/**
 * 告警记录页。
 *
 * 数据源：GET /api/v1/devices/{id}/alerts（支持 state/active 过滤，本页先展示全量）。
 * 注意：告警触发证据（evidence）由后端保存，页面不得用最新值反推历史告警原因。
 */
const deviceService = require('../../services/device.js')
const { formatRfc3339 } = require('../../utils/helpers.js')

/** 告警状态 -> 标签样式与文案（枚举值来自契约 §7） */
const STATE_META = {
  suspect: { tag: 'tag-amber', text: '疑似异常' },
  fire_warning: { tag: 'tag-red', text: '火情预警' },
  acknowledged: { tag: 'tag-amber', text: '已确认' },
  recovered: { tag: 'tag-green', text: '已恢复' },
}

Page({
  data: {
    loading: true,
    error: '',
    alerts: [],
  },

  onShow() {
    this.fetch()
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
    } catch (e) {
      this.setData({ loading: false, error: (e && e.message) || '加载失败' })
    }
  },
})
