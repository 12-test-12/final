/**
 * REST 请求封装（真实后端模式）。
 *
 * - 统一 JSON 编码、超时控制与错误信封解析（契约 §1.6 error envelope）。
 * - 业务错误统一抛 ApiError（code / message / statusCode），页面据此提示。
 * - Mock 模式不会走到本文件，由 services/mock/ 直接拦截。
 */
const { env } = require('../config/env.js')

/** 业务错误：code 对应契约错误码，如 invalid_threshold、device_not_found */
class ApiError extends Error {
  constructor(code, message, statusCode) {
    super(message)
    this.code = code
    this.statusCode = statusCode
  }
}

/** 拼接 baseUrl、path 与 query 参数 */
function buildUrl(base, path, query) {
  let url = base + path
  if (query && Object.keys(query).length > 0) {
    const qs = Object.keys(query)
      .map((k) => encodeURIComponent(k) + '=' + encodeURIComponent(String(query[k])))
      .join('&')
    url += '?' + qs
  }
  return url
}

/**
 * 发起请求。仅接受 2xx，其余按错误信封解析后抛 ApiError。
 * @param {string} path 以 /api/v1 开头的路径
 * @param {Object} [options] { method, data, header, query }
 */
function rawRequest(path, options = {}) {
  const { method = 'GET', data, header = {}, query } = options
  return new Promise((resolve, reject) => {
    wx.request({
      url: buildUrl(env.baseUrl, path, query),
      method,
      data,
      timeout: env.requestTimeout,
      header: Object.assign({ 'Content-Type': 'application/json; charset=utf-8' }, header),
      success(res) {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          resolve(res.data)
          return
        }
        const envelope = (res.data && res.data.error) || {}
        reject(new ApiError(envelope.code || 'http_' + res.statusCode, envelope.message || '请求失败', res.statusCode))
      },
      fail(err) {
        reject(new ApiError('network_error', err.errMsg || '网络错误', 0))
      },
    })
  })
}

module.exports = { rawRequest, ApiError }
