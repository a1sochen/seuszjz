# 东南大学讲座助手（seuszjz）

东南大学「学生素质讲座」预约系统的第三方客户端。Wails 3 + Vue 3 + TypeScript，界面基于 shadcn-vue（Tailwind CSS v4，黑白配色），一份代码编出两个形态：

- **Windows 桌面版**：单文件 `bin\seuszjz.exe`，双击即用，前端资源与验证码模型全部内嵌。
- **服务器版**：`-tags server` 构建的纯 HTTP 服务（无 GTK / WebView 依赖），打包成 Docker 镜像后用浏览器访问。

登录态、账号、密码、会话 Cookie 只存在浏览器的 `localStorage` 里，程序不会在运行的机器上生成任何配置或会话文件。

## 关于本项目

**本仓库的全部代码与文档均由 DeepSeek V4.1 Flash 在 DSH（DeepSeek Harness）中完成，DSH 未安装任何额外插件。** 需求由作者提出，实现、调试与文档撰写由模型完成；仓库内所有说明统一使用中文。

## 功能

- **统一身份认证登录**：CAS 账号密码 → aTrust 网关 → ehall 讲座系统，三步全自动；密码可用 RSA 公钥加密后提交。
- **免登录 / 自动登录**：登录成功后把账号、密码（AES-GCM 混淆）与会话 Cookie 写入 `localStorage`（键名 `seuszjz.state.v1`），下次启动直接复用；Cookie 失效时用保存的密码静默重登一次。
- **讲座列表**：实时同步，显示状态（可预约 / 未开放 / 已满 / 已结束 / 已预约 / 已打卡 / 已违约）、容量、时间地点主讲人；支持关键字搜索与状态筛选。
- **预约 / 取消**：弹窗展示验证码，可手输或让内置模型一键识别；「验证码错误」自动换一张，「操作频繁」提示稍后再试。
- **我的预约**：预约码、打卡状态、预约时间一目了然，未结束的预约可直接取消。
- **通知公告**：列表 + 展开正文，置顶公告高亮。
- **自动抢座**：选定目标讲座后轮询列表，开放瞬间自动取验证码并提交；支持自动识别 / 人工输入两条路径、限流冷却、完整运行日志，成功后窗口闪烁提醒。
- **设置**：账号、记住密码、自动登录、验证码自动识别、深色 / 浅色主题、抢座默认参数、清除本地登录信息。
- **纯 Go 验证码识别**：基于 [only9464/ddddocr-go](https://github.com/only9464/ddddocr) 做前向推理，不依赖 Python / onnxruntime / cgo，识别耗时约 100~150 ms。

## 环境要求

| 用途 | 依赖 |
| --- | --- |
| 运行桌面版 | Windows 10/11（WebView2 运行时，Win11 自带） |
| 运行服务器版 | 任意支持 Docker 的系统 |
| 构建 | Go 1.26+、Node.js 20+、[wails3 CLI](https://v3.wails.io/) v3.0.0-beta.34、npm；`task` 可选 |

运行时不依赖 Python、onnxruntime、CUDA，也不需要显卡。

## 构建与运行

桌面版（Windows）：

```powershell
wails3 dev                 # 开发模式：Vite 热更新 + Go 自动重编
wails3 task build          # 生产构建 → bin\seuszjz.exe
```

服务器版：

```bash
wails3 task build:server   # 本机平台的纯 HTTP 二进制 → bin/seuszjz-server
wails3 task build:docker   # Docker 镜像，默认 seuszjz:latest
wails3 task run:docker     # 构建并运行，浏览器打开 http://127.0.0.1:8080/
```

`wails3 task <名字>` 与系统里的 `task <名字>` 等价；`taskfile` 里只保留了 Windows 桌面打包和服务器镜像，macOS/iOS/NSIS/MSIX 之类的模板任务已删除。

> `main.go` 用 `go:embed` 内嵌 `frontend/dist`，所以直接 `go build` 前要先有前端产物：`cd frontend && npm ci && npm run build`（`wails3 task build` 会自动按顺序做）。

## 网络环境（校园网 / 校外）

校外访问 ehall 必须先过 `vpn.seu.edu.cn` 的 aTrust 网关；校园网本来就在内网，那个网关通常用不上，所以登录路径是可选的（设置页「账号 → 网络环境」）：

| 模式 | 行为 |
| --- | --- |
| 自动识别（默认） | 先匿名请求一次 `https://ehall.seu.edu.cn/`：校外会被网关 302 到 `vpn.seu.edu.cn/controller/v1/public/verify`，据此判断要不要过网关 |
| 校园网 | 只走直连：CAS → ehall，完全不碰 `vpn.seu.edu.cn` |
| 校外 | 先过 aTrust 网关，再访问 ehall（初版行为） |

两条路互为兜底，但只有「网络不可达」（DNS 失败、拨号超时、连接被拒）才换路；密码错误、票据为空这类业务失败直接报错，不做无谓重试。直连尝试另有 15 秒预算，判错了也不会让人干等。

两点副作用：校园网直连时拿不到网关的 `onlineInfo`，用户名会退化成学号；切换网络环境后旧会话可能失效，但**网络不通不会清掉本地登录信息**（日志里是 `网络不可用, 保留本地会话`），恢复网络后可以继续用。实际走的路径会体现在登录进度和日志里（`网络探测结果: campus` / `offcampus`）。

## 服务器模式

服务器版用 `-tags server` 编译：Wails 把窗口实现换成空壳（`Center` / `Flash` / `Focus` 等都是空操作），改为只跑 HTTP 服务，因此 `main.go` 里没有任何「服务器模式」分支，同一份代码两种构建即可。前端页面、`/wails/runtime` 绑定调用、WebSocket 事件在普通浏览器里都能用，所以镜像里不需要 GTK / WebKitGTK。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `WAILS_SERVER_HOST` | `localhost`（镜像内 `0.0.0.0`） | 监听地址 |
| `WAILS_SERVER_PORT` | `8080` | 监听端口 |

账号与会话都在浏览器 `localStorage` 里，服务器上不需要挂卷；容器重建后只要浏览器站点数据还在，就仍然免登录。只想本机访问时：

```bash
docker run -d --rm -p 127.0.0.1:8080:8080 seuszjz:latest
```

> 服务器模式下没有额外认证，能访问端口的人就能用你的登录态预约 / 取消。请只在本机或内网使用；公网部署务必加一层带认证的反向代理。

## 目录结构

```
main.go                 入口：注册服务、创建窗口（服务器构建下是空壳）、注入事件
service.go              服务层：会话 / 讲座 / 抢座三组 API + 远端请求串行化
service_types.go        前后端共享的数据结构（TS 绑定由此生成）
service_test.go         服务层测试（localStorage 快照的接管、旧版文件导入）
internal/seu/           讲座系统协议客户端（CAS 登录、aTrust 网关、ehall 接口）
internal/store/         账号与会话的内存快照（序列化成 JSON 交给前端存 localStorage）
internal/ocr/           验证码识别适配层（内部用 ddddocr-go）
internal/grab/          自动抢座引擎（状态机 + 轮询 + 验证码通道）
frontend/               Vue 3 界面（shadcn-vue + Tailwind v4，src/components/ui 为组件库）
                        src/assets/seu-logo.svg 是官方校标矢量图，界面与程序图标都用它
build/                  Wails 构建配置：windows 打包、docker 镜像、图标与元信息
scripts/                逆向阶段的两个 Python 原始脚本（仅供对照，不参与构建）
.github/                发布流程：打 tag 后自动构建四个产物并推送 Docker 镜像
```

前端调用 Go 的 TypeScript 绑定由 `wails3 generate bindings` 生成到 `frontend/bindings/`，构建时自动刷新。

界面左上角、登录页、窗口图标与 favicon 使用的都是东南大学官方校标。程序图标与 favicon 由 `frontend/src/assets/seu-logo.svg`（彩色校标，来自学校视觉识别系统的矢量素材）渲染导出：`build/appicon.png`（1024×1024）、`frontend/public/favicon.png`（128×128）、`build/windows/icon.ico`（`wails3 generate icons` 生成）。校标版权归东南大学所有，这里只作为本工具的标识使用，如与学校的视觉识别规范冲突，以学校规范为准。

## 发布

发布由 `.github/workflows/release.yml` 完成，两种触发方式：

```bash
# 推荐：打 tag 推送，版本号取自 tag
git tag v1.2.3
git push origin v1.2.3
```

也可以在 Actions 页面手动运行「发布」：填写版本号会构建并创建 Release；版本号留空只构建产物、不发布（版本号记为 `0.0.0-dev-<短提交号>`）。

流程依次做四件事：解析版本号 → 检查（前端编译 + `go vet` + `go test`）→ 四个平台并行构建 → 创建 Release。产物如下：

| 产物 | 平台 | 说明 |
| --- | --- | --- |
| `seuszjz.exe` | Windows | 桌面版，双击运行，无控制台窗口 |
| `seuszjz-server.exe` | Windows | 服务版，命令行运行后浏览器访问 |
| `seuszjz-server-linux-amd64.tar.gz` | Linux | 服务版（x86_64） |
| `seuszjz-server-linux-arm64.tar.gz` | Linux | 服务版（ARM64） |

同时构建 `linux/amd64` 与 `linux/arm64` 两个架构的镜像推到 GHCR：

```bash
docker run -d -p 8080:8080 ghcr.io/only9464/seuszjz:latest
```

版本号会写进三个地方，保证「设置页显示的版本 = tag」：`.github/scripts/set-version.py` 改写 `build/config.yml`、`build/windows/info.json`（exe 属性，四段式）、`frontend/package.json`；Go 侧的 `main.Version` 由构建参数 `APP_VERSION` 通过 `-ldflags "-X main.Version=..."` 注入（Dockerfile 里是 `--build-arg APP_VERSION=...`）。本地想模拟发布构建：

```bash
APP_VERSION=1.2.3 wails3 task build        # 桌面版
APP_VERSION=1.2.3 wails3 task build:server # 服务版
```

两条与仓库设置有关的提醒：

- Actions 的默认工作流权限是只读，工作流里已经自带 `permissions: contents: write / packages: write`，不需要额外改仓库设置。
- 第一次推送镜像后，GHCR 上的包默认是 **private**，需要在仓库右侧 Packages 里把它改成 public，否则别人拉取要先登录。

## 数据与隐私

- 学号、密码（混淆后）、会话 Cookie **只存在浏览器 `localStorage`**；Go 端只在内存里持有它们，不会在运行程序的机器上创建配置文件。桌面版的存储由 WebView2 的用户数据目录（`%APPDATA%\seuszjz\webview`）承载，服务器版由你自己的浏览器承载。
- 密码用 AES-GCM 混淆后才写入，但密钥编译在程序里，只能防「一眼看到明文」，**不是安全边界**：能读到那份 localStorage 的人同样能拿到 Cookie。
- 清除方式：设置页「本地登录信息 → 清除」，或删掉浏览器的站点数据。
- 所有请求直连学校系统，不经过任何第三方服务器；本工具不收集任何数据。
- 从旧版本升级：如果 `%APPDATA%\seuszjz\`（或 `SEUSZJZ_DATA_DIR` 指定的目录）里还有 `config.json` / `session.json`，首次启动会自动导入一次并转存进 localStorage，**不会删除**旧文件，可自行清理。

## 验证码识别

识别用 [only9464/ddddocr-go](https://github.com/only9464/ddddocr)：纯 Go 的 ddddocr 移植，权重（20.4 MB 主干 + 分类头）用 `go:embed` 内嵌在依赖库里，图片解码、Lanczos 缩放、conv/BiLSTM 前向与 CTC 去重都在库内完成。`internal/ocr` 只是一层薄适配：

```go
rec, err := digits.New()          // 权重进程内只加载一次
text, err := rec.Recognize(jpeg)  // 入参是图片原始字节
```

所以仓库里没有 `weights.bin`，也不需要 Python 导出模型。换字符集只需改子包：`digits`（空白 + 0-9，当前用法）、`alnum`（字母数字）、`full`（完整字符集，权重 33 MB）。

依赖里有一条**必须保留**的 `replace`：

```
require github.com/only9464/ddddocr-go v0.0.1
replace github.com/only9464/ddddocr-go => github.com/only9464/ddddocr v0.0.1
```

上游 `go.mod` 声明的 module 路径是 `.../ddddocr-go`，而仓库地址是 `.../ddddocr`，不加 replace 会报 `module declares its path as ... but was required as ...`。同样地，不要在任何脚本或 Dockerfile 里批量删除 `replace`。

模型来自 [ddddocr](https://github.com/sml2h3/ddddocr)（MIT 许可）。`go test ./internal/ocr -v` 会用 `testdata/` 里的真实验证码跑端到端、并发与超时用例。

## 关于 scripts/

`scripts/seu_lecture_proto.py` 与 `scripts/grab.py` 是本项目协议实现的原始逆向成果，Go 端 `internal/seu`、`internal/grab` 是它们的移植版本。保留它们只为对照协议细节（CAS 表单、aTrust 跳转、验证码接口），**不参与构建与运行**，也不需要安装 Python 依赖。

## 常见问题

- **打不开 / 白屏**：确认已安装 WebView2 运行时（Win11 自带，Win10 需装 Evergreen Bootstrapper）。
- **提示「验证码错误」**：验证码约 1 分钟有效，取图后请尽快提交；识别错了点图片换一张。
- **提示「操作频繁」**：学校系统有限流，等冷却时间结束再试，不要连续点击。
- **换电脑后要重新登录**：正常，登录态只存在浏览器本地，不会跟着程序走。

## 参与贡献

- 提 issue 请说明系统版本、复现步骤与日志（日志里若含 Cookie / 学号请先打码）。
- 提交代码前请跑 `go vet ./... && go test ./...` 与 `cd frontend && npm run build`；`go build ./...` 需要先构建过前端（见上）。
- 请不要把任何真实的账号、Cookie、验证码图片或包含个人信息的截图提交进仓库。

## 许可证

[MIT](LICENSE)。验证码模型权重来自 ddddocr（MIT），Wails 与 shadcn-vue 分别遵循各自许可证。

## 免责声明

本项目仅用于个人预约讲座的自动化尝试，与东南大学官方无关。请遵守学校相关规定，不要用于抢票、代约等违规用途；使用本工具产生的一切后果由使用者自行承担。
