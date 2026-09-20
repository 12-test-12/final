/**
 * 应用入口。
 *
 * globalData.deviceId: 当前绑定的监测终端编号。硬件仅一台，按接口契约示例固定为 MCU001。
 * globalData.env: 环境配置，见 config/env.js（含 useMock 开关与后端地址）。
 */
const { env } = require('./config/env.js')

App({
  globalData: {
    deviceId: 'MCU001',
    env,
  },
  onLaunch() {
    // 仅日志用途：确认当前数据源，便于联调排查
    console.log('[app] 数据源:', env.useMock ? 'Mock（本地假数据）' : '真实后端', env.baseUrl)
  },
})
