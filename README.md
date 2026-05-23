BKNetwork
=========

BKNetwork 是一个用于 Windows 的轻量级本地后台服务模板，带有内置的 Web 管理界面，用于管理网卡绑定、Cloudflare WARP 等功能。

**下载与安装**

在 Release 页面下载最新版 BKNetwork，可直接运行 `bknetwork.exe`

**快速上手**

在开发机器上可用以下命令直接运行（无需提前构建）：

```powershell
cd BKNetwork
go run ./cmd/bknetwork
```

运行后会在本地监听默认地址（参见代码中的 Server 配置），并将 `web` 目录中的前端文件作为静态资源提供。

**构建二进制**

在项目根目录执行（会在当前目录生成 `bknetwork.exe`）：

```powershell
cd BKNetwork
go build -o bknetwork.exe ./cmd/bknetwork
```

在 Windows 上构建并打包为服务或分发给别的机器时，建议在与目标平台相同的环境中构建（比如使用带有相同 GOOS/GOARCH 的交叉编译或在目标 Windows 主机上构建）。

示例（在非 Windows 上交叉编译 Windows 可执行文件）：

```bash
# 在 Linux/macOS 环境交叉编译为 Windows amd64
GOOS=windows GOARCH=amd64 go build -o bknetwork.exe ./cmd/bknetwork
```

**以服务方式安装（Windows）**

生成 `bknetwork.exe` 后，使用管理员权限运行安装命令：

```powershell
# 以管理员身份打开 PowerShell
.\bknetwork.exe install
.\bknetwork.exe start
```

程序使用 `github.com/kardianos/service` 做为服务包装，安装/启动/停止命令均由可执行文件暴露（参见 `cmd/bknetwork` 目录下的实现）。

**HTTP / WebSocket 接口**

- 静态 Web UI：根路径（`/`）会提供 `web` 目录下的文件。
- REST 状态接口：`/api/v1/status` — 返回最近一次网络快照与服务状态。
- 控制接口：`/api/v1/switch`（切换 IPv4/IPv6）、`/api/v1/warp`（控制 warp-cli），均为 POST 请求。
- 实时事件：WebSocket 路径为 `/ws`，会发送 `hello`、`network.status`、`heartbeat` 等事件。

注意：改变网络绑定或控制 `warp-cli` 的命令需要以管理员权限执行，接口会在权限不足或命令不可用时返回错误信息并通过 WebSocket 发布事件。

**常见问题与排查**

- 首次打开页面状态加载慢：服务在采集网络快照时会调用若干 PowerShell 和外部命令（如 `warp-cli`），可能耗时。建议在疑难排查时直接在服务器主机上运行 `go run ./cmd/bknetwork` 并观察输出日志。
- `warp-cli not found`：如果未安装 Cloudflare WARP 客户端，`/api/v1/warp` 会返回错误并在日志中给出提示。安装后确保 `warp-cli` 在 PATH 中可访问。
- 权限不足：修改网卡绑定等操作需要管理员权限，若在非管理员上下文运行会收到 403 或相应错误信息。

**开发与测试**

仓库包含部分单元测试，可用以下命令运行测试：

```bash
cd BKNetwork
go test ./...
```

**目录结构（相关）**

- `cmd/bknetwork` — 程序入口与平台相关的包装代码（服务安装、桌面集成等）。
- `internal/handlers` — HTTP 处理器，包含网络快照采集、warp 控制等逻辑。
- `internal/events` — 事件总线，用于将事件广播到 WebSocket 订阅者。
- `web/` — 前端静态资源（UI、SVG、CSS、JS）。

**许可**

本项目采用 MIT 许可。