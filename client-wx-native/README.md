# WeChat Native Client

该目录是客户端方案 B：使用微信官方原生小程序 UI、生命周期和 API 的独立实现，后续作为 KMP MiniApp 方案的工程对照 baseline。

## Current Status

当前目录包含微信开发者工具生成的示例骨架：应用配置、首页、日志页和工具函数。示例中的登录、头像昵称与日志展示不是本项目正式业务；本轮保留原状，不删除、不扩展，也不据此冻结任何产品设计。

## Project Boundary

- 后续与 KMP 客户端实现相同业务能力，并使用相同 Backend API、硬件数据源和验收场景。
- 保持微信原生工程方式和真实开发成本，不为了匹配 KMP 目录结构而人为改造。
- 不依赖 `client-kmp` 的内部实现；跨客户端只共享已确认的外部契约和需求事实。
- 当前阶段不实现页面、网络层或业务逻辑。

使用微信开发者工具打开本目录即可检查现有原生小程序骨架。`project.config.json` 中已有项目配置；其中 AppID 等环境相关值应由团队确认后再用于正式开发与发布。

## Collaboration Workflow

```sh
git clone <repository-url>
cd final
git switch main
git pull --ff-only
git switch -c feat/wx-monitor-dashboard

# 在微信开发者工具中完成编译、模拟器和必要的真机验证
git add client-wx-native
git commit -m "feat(wx): add monitor dashboard"
git push -u origin feat/wx-monitor-dashboard
```

分支必须采用 `<type>/wx-<complete-description>`，例如 `fix/wx-threshold-validation`。PR 关联 Multica issue，说明页面/组件、微信 API、Backend 契约和开发者工具/真机验证结果；经审核和检查通过后方可合并。`project.private.config.json` 等本机私有文件不得提交。
