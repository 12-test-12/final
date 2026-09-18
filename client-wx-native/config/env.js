/**
 * 环境配置（唯一修改点）。
 *
 * useMock: true  -> 所有请求走本地 Mock（services/mock/），后端未就绪期间使用。
 * useMock: false -> 请求真实后端，地址取 baseUrl / wsUrl。
 *
 * 联调提示：微信开发者工具需勾选「不校验合法域名」；真机调试时
 * baseUrl 改成电脑的局域网 IP（如 http://192.168.1.10:8080）。
 */
const env = {
  useMock: true,
  baseUrl: 'http://localhost:8080',
  wsUrl: 'ws://localhost:8080',
  requestTimeout: 5000,
}

module.exports = { env }
