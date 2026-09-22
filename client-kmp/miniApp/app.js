const runtime = require('./runtime.js')
const config = require('./config.js')

App({
  onLaunch() {
    runtime.configure(config.baseUrl, config.deviceId)
  },
})
