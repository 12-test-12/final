/**
 * 自定义底部导航栏。
 *
 * 微信原生 tabBar 无法实现“激活项放大抬起”的动效，
 * 因此改用自定义 tabBar（app.json 中 tabBar.custom = true）。
 * 每个 tab 页面需在 onShow 中调用 this.getTabBar().setData({ selected }) 同步高亮项。
 */
Component({
  data: {
    selected: 0,
    list: [
      {
        pagePath: '/pages/dashboard/dashboard',
        text: '监控',
        code: 'MONITOR',
        icon: '/assets/icons/dashboard.png',
        activeIcon: '/assets/icons/dashboard-active.png',
      },
      {
        pagePath: '/pages/trends/trends',
        text: '趋势',
        code: 'TREND',
        icon: '/assets/icons/trends.png',
        activeIcon: '/assets/icons/trends-active.png',
      },
      {
        pagePath: '/pages/alerts/alerts',
        text: '告警',
        code: 'ALERT',
        icon: '/assets/icons/alerts.png',
        activeIcon: '/assets/icons/alerts-active.png',
      },
      {
        pagePath: '/pages/settings/settings',
        text: '设置',
        code: 'CONFIG',
        icon: '/assets/icons/settings.png',
        activeIcon: '/assets/icons/settings-active.png',
      },
    ],
  },

  methods: {
    onTap(e) {
      const index = Number(e.currentTarget.dataset.index)
      const item = this.data.list[index]
      if (index === this.data.selected) return
      wx.switchTab({ url: item.pagePath })
      this.setData({ selected: index })
    },
  },
})
