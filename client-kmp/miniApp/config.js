/**
 * Runtime configuration for the WeChat host.
 *
 * `baseUrl` must be reachable *from the device running the mini program*:
 *
 * - WeChat DevTools on this machine: `http://127.0.0.1:8080` works, but the
 *   project must have "不校验合法域名" (skip domain check) enabled under
 *   Details → Local Settings, because plain HTTP to a raw IP/loopback is not a
 *   configured legal domain.
 * - Real phone: `127.0.0.1` is the *phone*, so it can never reach your Mac.
 *   Replace it with the LAN address of the machine running the backend, e.g.
 *   `http://192.168.1.23:8080`, and keep the phone on the same Wi-Fi or hotspot.
 *
 * A LAN address is deployment-specific, so it is deliberately not committed.
 * Production must use HTTPS on a domain filed in the mini program console;
 * `wx.request` refuses plain HTTP outside developer mode.
 */
module.exports = {
  baseUrl: 'http://127.0.0.1:8080',
  deviceId: 'MCU001',
}
