This is a Kotlin Multiplatform project targeting Android, iOS.

* [/iosApp](./iosApp/iosApp) contains an iOS application. Even if you’re sharing your UI with Compose Multiplatform,
  you need this entry point for your iOS app. This is also where you should add SwiftUI code for your project.

* [/shared](./shared/src) is for code that will be shared across your Compose Multiplatform applications.
  It contains several subfolders:
    - [commonMain](./shared/src/commonMain/kotlin) is for code that’s common for all targets.
    - Other folders are for Kotlin code that will be compiled for only the platform indicated in the folder name.
      For example, if you want to use Apple’s CoreCrypto for the iOS part of your Kotlin app,
      the [iosMain](./shared/src/iosMain/kotlin) folder would be the right place for such calls.
      Similarly, if you want to edit the Desktop (JVM) specific part, the [jvmMain](./shared/src/jvmMain/kotlin)
      folder is the appropriate location.

### Running the apps

Use the run configurations provided by the run widget in your IDE's toolbar. You can also use these commands and
options:

- Android app: `./gradlew :androidApp:assembleDebug`
- iOS app: open the [/iosApp](./iosApp) directory in Xcode and run it from there.

### Running tests

Use the run button in your IDE's editor gutter, or run tests using Gradle tasks:

- Android tests: `./gradlew :shared:testAndroidHostTest`
- iOS tests: `./gradlew :shared:iosSimulatorArm64Test`

---

Learn more
about [Kotlin Multiplatform](https://www.jetbrains.com.cn/en-us/help/kotlin-multiplatform-dev/get-started.html)…

## Project Role and Boundary

该目录是客户端方案 A。当前工程是一个已生成的 Kotlin Multiplatform Android/iOS 模板，包含 Compose 示例 UI 和示例测试；这些内容是现状，不代表最终业务或最终目标平台已经实现。

后续目标是在 Android 与微信小程序之间共享真正可共享的客户端业务逻辑：

- `commonMain` 只承载跨平台业务规则、状态转换和可共享模型，不承载 Android 或微信 UI。
- Android UI 与微信 Host UI 分别由各自平台消费共享逻辑。
- `miniappMain` 由 Mini App Gradle 插件提供，只用于微信/JavaScript Runtime 的平台适配，不承载 WXML/WXSS。
- 平台 API 通过接口或 Adapter 隔离；不把共享层演进成自研跨平台 UI Framework。

当前已通过 SDK 插件引入 Mini App target，但不实现业务页面，也不冻结 Backend API。现有 iOS 模板作为附加目标保留；本轮不删除或重构。

## Mini App SDK Dependency

`settings.gradle.kts` 通过本地 composite build 分别解析未发布的 Gradle 插件与 runtime 坐标。
`shared` 应用 `io.github.bobcgn.miniapp` 后，插件会提供 Mini App target，在 `shared/src`
下创建 `miniappMain/kotlin` 与 `miniappTest/kotlin`，并仅把 runtime SDK 接入 `miniappMain`。

## Collaboration Workflow

```sh
git clone <repository-url>
cd final
git switch main
git pull --ff-only
git switch -c feat/kmp-monitor-dashboard

cd client-kmp
./gradlew check
cd ..

git add client-kmp
git commit -m "feat(kmp): add monitor dashboard state"
git push -u origin feat/kmp-monitor-dashboard
```

分支必须采用 `<type>/kmp-<complete-description>`，例如 `fix/kmp-telemetry-parsing`。PR 关联 Multica issue，说明受影响 source set、共享边界、API 契约及各目标验证结果；经审核和检查通过后方可合并。不得把微信 WXML/WXSS 或平台 UI 放入 `commonMain`。
