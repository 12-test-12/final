# client-kmp

实验室微环境动环监控与早期火情预警 —— 客户端方案 A（Kotlin Multiplatform）。

本工程让 **Android** 与 **微信小程序** 消费同一套业务运行时：接口约定、状态派生、
校验和错误映射只写一次，两端的标签、颜色语义和数值格式由共享层输出，避免两端各自
解释同一份 Backend 契约而产生分歧。

事实源：

- API 契约：[`../docs/api/openapi.yaml`](../docs/api/openapi.yaml)
- 设备协议：[`../docs/device-protocol.md`](../docs/device-protocol.md)
- 本模块约束：[`AGENTS.md`](./AGENTS.md)，根规则：[`../AGENTS.md`](../AGENTS.md)

---

## 一、架构与 source set 边界

| Source set | 内容 | 禁止内容 |
| --- | --- | --- |
| `commonMain` | 纯业务运行时：模型、解析、状态派生、校验、错误映射 | Compose、WXML/WXSS、任何平台 UI |
| `miniappMain` | 微信/JS Runtime 平台适配与 CommonJS 导出 | 微信 UI、WXML/WXSS |
| `androidMain` | Android 传输适配（`HttpURLConnection`）+ Compose 四页面 | 共享业务规则 |
| `iosMain` | iOS 入口（Compose），保留既有模板 | — |
| `miniApp/`（仓库目录，非 Gradle source set） | 微信 Host UI：WXML/WXSS/JS | 共享业务规则 |

关键约束：

- `commonMain` 只保存真正可共享的业务逻辑，不承载 Compose 或 WXML/WXSS。
- Android UI 与 MiniApp Host UI 完全独立，只共用 `commonMain` 的运行时与 **展示模型**。
- 平台能力（HTTP、幂等键）通过 `MonitoringPlatform` 接口隔离，`commonMain` 不引用任何平台 API。
- 不复制 `client-wx-native` 的内部实现；只参考其视觉与交互语义。

### 共享业务逻辑（两端共同消费）

`shared/src/commonMain/kotlin/org/example/client_kmp/monitoring/`

| 文件 | 职责 |
| --- | --- |
| `Models.kt` | 契约模型：`DeviceStatus`、`TelemetryPoint`、`AlertEvent`、`Thresholds`、`CommandStatus`、错误信封 |
| `Transport.kt` | `HttpRequest` / `HttpResponse` / `MonitoringPlatform` 平台边界、`MonitoringException` |
| `Presentation.kt` | 仪表盘派生、趋势统计、告警展示模型、阈值校验、命令生命周期措辞、`Tone` 颜色 token |
| `MonitoringClient.kt` | 端点路径、序列化、幂等键、错误信封映射、202 控制闭环轮询 |

两端**只有一处**业务实现：`MonitoringClient` + `MonitoringPresentation`。
Android UI 和 WXML 都只负责排版。

### 平台适配

- Android：`androidMain/.../AndroidMonitoringPlatform.kt`，阻塞 IO 固定在 `Dispatchers.IO`，
  幂等键用 `UUID.randomUUID()`。
- 微信：`miniappMain/.../MiniAppMonitoringExports.kt`，网络走 MiniApp SDK 的
  `MiniAppExports.networkRequest`，幂等键用「毫秒时间戳 + 进程内自增序号」
  （`crypto.randomUUID` 并非所有基础库可用）。

---

## 二、MiniApp bundle 与导出结构

`./gradlew prepareMiniAppHost` 把插件产出的 CommonJS 发行包同步到 `miniApp/kotlin/`。

**实际入口文件（已核对，非假设）：**

```
miniApp/kotlin/client-kmp-shared-miniapp.js
```

文件名来自 Gradle 工程路径 `:shared` → `client-kmp-shared-miniapp`，并由同目录
`package.json` 的 `main` 字段声明。

**实际导出路径（已核对）：**

```js
bundle.org.example.client_kmp.monitoring.LabMonitorExports
```

`@JsExport` object 挂在 Kotlin 包路径下，**不在** bundle 根。`miniApp/runtime.js`
封装了这一路径，页面代码不直接触碰 `./kotlin`。

**导出成员：**

| 成员 | 说明 |
| --- | --- |
| `configure(baseUrl, deviceId)` | 同步，设置后端地址；须在任何数据调用前执行 |
| `dashboard()` | 返回 DashboardView JSON |
| `trends(limit)` | 返回 TrendsView JSON（按时间升序） |
| `alerts(limit)` | 返回 AlertsView JSON |
| `settings()` | 返回 SettingsView JSON |
| `commandStatus(requestId)` | 读取命令生命周期 |
| `awaitCommandOutcome(requestId)` | 等待设备确认；超时返回 `null`（未确认，非失败） |
| `mute(muted)` | 下发静音/恢复 |
| `updateThresholds(t, h, g)` | 校验并下发阈值 |

所有 `suspend` 函数在 JS 侧表现为 **Promise**；失败会 reject 成一个普通 `Error`，
`message` 可直接展示，并附带 `code` / `statusCode`。

> `miniApp/kotlin/` 是**生成目录**，已在 `miniApp/.gitignore` 中排除，禁止提交。
> 其中 Compose Multiplatform 插件附带的浏览器渲染资源（`skiko.wasm`、`skiko*.mjs`、
> ES module shim、source map）已由 `prepareMiniAppHost` 排除，并配有校验：
> 一旦这些文件出现或入口文件缺失，构建即失败。

### 编译产物分布

| 位置 | 内容 | 是否交付微信 | 版本控制 |
| --- | --- | --- | --- |
| `miniApp/` | 微信工程：页面/WXSS/JS + `kotlin/` bundle，**自包含** | 是 | 提交（`kotlin/` 除外） |
| `build/js` | Kotlin/JS **测试**工具链：node_modules、测试包 | 否 | 忽略 |
| `build/kotlin-js-store` | Kotlin/JS 的 yarn lock store | 否 | 忽略 |
| `shared/build/miniapp` | 插件产出的 bundle 暂存区 | 否 | 忽略 |
| `shared/build/processedResources/miniapp` | 处理后的资源 | 否 | 忽略 |
| `.kotlin/`、`shared/build/kotlin` | 编译缓存 | 否 | 忽略 |

两点保证：

1. **`client-kmp/` 根目录不再出现任何小程序或 JS 产物。**
   Kotlin/JS 默认把 yarn store 放在 `<root>/kotlin-js-store`，既不是构建目录、
   也不属于微信交付物。已在根 `build.gradle.kts` 中通过
   `YarnRootExtension.lockFileDirectoryProperty` 改到 `build/kotlin-js-store`。
   本项目没有 npm 运行时依赖（`package.json` 的 `dependencies` 为空），
   该 store 只固定跑测试用的 TypeScript 版本，放进 `build/` 无副作用。
2. **`miniApp/` 自包含，可整体拷贝/上传。**
   `prepareMiniAppHost` 会解析 `miniApp/` 下所有 `.js` 的 `require('./…')`，
   若有相对引用落在 `miniApp/` 之外即构建失败——避免「本地能跑、拷走后崩」。

---

## 三、运行与配置

### 后端地址

| 运行环境 | base URL | 原因 |
| --- | --- | --- |
| Android 模拟器 | `http://10.0.2.2:8080` | `10.0.2.2` 是模拟器指向宿主机的固定别名 |
| 微信开发者工具（本机） | `http://127.0.0.1:8080` | 工具与后端同机 |
| 微信真机 | `http://<Mac 在手机热点/局域网中的 IP>:8080` | 真机的 `127.0.0.1` 是手机自己，永远访问不到 Mac |

- Android：`shared/src/androidMain/.../App.kt` 的 `EMULATOR_BASE_URL`。
- 微信：`miniApp/config.js` 的 `baseUrl` / `deviceId`。**局域网 IP 属于部署环境信息，
  不要写进仓库**；真机调试时本地改成宿主机 IP 即可。

### Android

```bash
cd client-kmp
./gradlew --no-configuration-cache :androidApp:assembleDebug
```

产物：`androidApp/build/outputs/apk/debug/androidApp-debug.apk`

安装到已启动的模拟器：

```bash
adb install -r androidApp/build/outputs/apk/debug/androidApp-debug.apk
```

`AndroidManifest.xml` 已声明 `INTERNET` 权限，并允许本地明文 HTTP（仅用于开发调试）。

### 微信小程序

```bash
cd client-kmp
./gradlew --no-configuration-cache prepareMiniAppHost
```

然后在**微信开发者工具**中导入目录：

```
client-kmp/miniApp
```

导入前必须确认：

1. 已执行 `prepareMiniAppHost`，`miniApp/kotlin/` 存在且非空。
2. 「详情 → 本地设置」勾选 **不校验合法域名、web-view（业务域名）、TLS 版本以及 HTTPS 证书**，
   否则开发工具会拦截到 `http://` 明文地址的请求。
3. 真机预览时把 `config.js` 的 `baseUrl` 改成 Mac 的局域网 IP。

生产环境必须使用 HTTPS 域名并在小程序后台配置合法域名；`wx.request` 在非开发者模式下拒绝明文 HTTP。

> **包体积**：`miniApp/kotlin/` 约 2.3 MB。微信主包上限 2 MB，请依赖开发者工具的
> **上传时压缩/混淆**（`project.config.json` 已开启 `minified`），或后续将该目录拆到分包。
> 这是当前已知风险，不是已解决问题。

---

## 四、测试与覆盖率

```bash
cd client-kmp

# 共享逻辑 + Android 平台适配（JVM）
./gradlew --no-configuration-cache :shared:testAndroidHostTest

# 共享逻辑 + MiniApp 平台适配（Node/JS，微信侧运行时）
./gradlew --no-configuration-cache :shared:miniappTest

# MiniApp 运行时不得携带 Compose/Skiko
./gradlew --no-configuration-cache :shared:checkMiniAppHostBoundary

# 覆盖率报告（Kover）；koverVerify 对共享逻辑执行 80% 行覆盖下限
./gradlew --no-configuration-cache :shared:koverXmlReport :shared:koverVerify

# 全量
./gradlew --no-configuration-cache check
```

覆盖率口径：仅统计 `org.example.client_kmp.monitoring`（两端共同的业务规则），
排除编译器生成的嵌套类。Compose 页面、WXML Host 与生成 bundle 属于 Host 代码，
不计入共享规则覆盖率。

### 已验证目标（本轮实际执行）

| 项目 | 结果 |
| --- | --- |
| `:shared:testAndroidHostTest` | 84 tests，0 failures |
| `:shared:miniappTest`（Node/JS） | 77 tests，0 failures |
| `:shared:iosSimulatorArm64Test` | 72 tests，0 failures（仅共享逻辑） |
| 共享业务逻辑行覆盖率 | 100%（405 行），方法/类 100% |
| `:shared:checkMiniAppHostBoundary` | PASS |
| `:androidApp:assembleDebug` | PASS，产出 debug APK |
| `./gradlew check` | PASS |

Android 平台测试对真实 loopback HTTP 服务发起请求，覆盖 `HttpURLConnection` 的
`inputStream` / `errorStream` 分支与请求体写出，不使用 mock 替代网络边界。

**未验证**：Android 真机/模拟器上的 UI 交互、微信开发者工具中的实际渲染与真机网络联通。
本轮只证明编译、构建、共享逻辑测试和 JS 运行时导出边界；**不得把「能编译」当成「真机通过」**。

### iOS

- iOS target（`iosArm64`、`iosSimulatorArm64`）保留，未删除。
- 本轮**不验证 iOS UI**：当前 macOS 27.2 环境不作为 iOS 界面验收依据，iOS Compose 页面
  与 `iosApp` 未构建、未运行。
- 唯一与 iOS 有关的已验证项是 `:shared:iosSimulatorArm64Test`，它只跑共享业务逻辑，
  不覆盖任何 iOS UI 行为。

---

## 五、当前不支持 / 未完成

- **实时推送未接入**：契约中 `/ws/v1/...` WebSocket 与 `WsEnvelope` 尚未在客户端实现，
  当前仅 REST 轮询（仪表盘 3 秒）。
- **告警确认（acknowledge）未实现**：阶段一无该端点，`AlertState` 因此不含 `acknowledged`。
- **趋势图为数据列表**：未引入图表库，按后端升序输出采样序列与统计摘要；
  超过 12 条时界面只预览末尾若干条。
- **历史查询窗口固定**：未暴露 `from` / `to` / `cursor` 参数，仅用 `limit`。
- **鉴权未接入**：契约的 `BearerAuth` 为阶段一占位，后端本地以 `AUTH_MODE=none` 运行，
  客户端未发送 Token。
- **小程序包体积**：见上节「包体积」风险。
- **真机与开发者工具渲染**：未执行。

---

## 六、协作流程

```bash
git switch main && git pull --ff-only
git switch -c feat/kmp-monitor-dashboard

cd client-kmp && ./gradlew check && cd ..

git add client-kmp
git commit -m "feat(kmp): add monitor dashboard state"
git push -u origin feat/kmp-monitor-dashboard
```

分支必须采用 `<type>/kmp-<complete-description>`，例如 `fix/kmp-telemetry-parsing`。
PR 关联 Multica issue，说明受影响 source set、共享边界、契约影响及各目标验证结果。
不得把微信 WXML/WXSS 或平台 UI 放入 `commonMain`，不得在 `main` 上直接提交。

---

了解更多：[Kotlin Multiplatform 文档](https://www.jetbrains.com.cn/en-us/help/kotlin-multiplatform-dev/get-started.html)
