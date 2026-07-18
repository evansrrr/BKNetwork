<div align="center">
<img alt="logo" height="100" width="100" src="web/favicon.svg" />
<h2> BKNetwork </h2>
<p> 校园网v6免流 </p>

[tg](https://t.me/ernst_loosen_bot/) | [实现原理](https://ich.cc.cd/2026/05/11/mianliu/) | [Github](https://github.com/evansrrr/BKNetwork) | [Gitee](https://gitee.com/evansrrr/BKNetwork)

<br />

[![Stars](https://img.shields.io/github/stars/evansrrr/BKNetwork?style=flat)](https://github.com/evansrrr/BKNetwork/stargazers)
[![Version](https://img.shields.io/github/v/release/evansrrr/BKNetwork)](https://github.com/evansrrr/BKNetwork/releases)
[![License](https://img.shields.io/github/license/evansrrr/BKNetwork)](https://github.com/evansrrr/BKNetwork/blob/dev/LICENSE)
[![Issues](https://img.shields.io/github/issues/evansrrr/BKNetwork)](https://github.com/evansrrr/BKNetwork/issues)

</div>

![preview.webp](https://imgs.ich.cc.cd/file/public/1784371816472_preview.webp)

## 说明

**仅支持 Win 11 x64**

在 [Releases](https://github.com/evansrrr/BKNetwork/releases/latest) 下载最新带版本号的 `.zip` ，解压后运行 `bknetwork.exe`，首次打开需要点击系统弹窗中 `更多信息` 并确定继续运行。这是因为软件没有微软签名，不必担心。如果遇到提示需要管理员权限，请选择同意。

运行软件后会在系统托盘中显示图标，单击可打开界面（`http://localhost:13335`），右键点击可 `退出` 。

**免流模式** 包含 `Warp` 和 `DNS64` 两种，开一个就行。切换通常10s内完成，视网络状态好坏

`Warp 免流模式` 需要安装 Cloudflare Warp，首次运行选择左侧 "Private browsing"，同意使用条款继续，重启后可以使用 `Warp 免流模式`。首次使用请在高级模式查看目标网卡，确认自动选择的是上网网卡，例如 WiFi/WLAN，而**不是**Warp网卡

Cloudflare Warp 软件下载页面（1.1.1.1）大陆地区目前无法访问，可以直接[点此下载Windows最新版本](https://1111-releases.cloudflareclient.com/win/latest)，这是官方下载链接

对于北科校园网，`USTB-Student` 和 `USTB-V6` 支持免流，`USTB_Wi-Fi` 不支持。换句话说，免流前提是连接 `USTB-Student` 或 `USTB-V6`，校园网账号认证成功，并且电脑可以正常获取 ipv6 地址（[testipv6](https://www.testipv6.cn/)）

**高级模式** 更详细的网络状态与控制选项

**设置** 控制软件开机自启、静默启动、Warp 客户端开机自启

*注：退出软件或开关机时不会自动恢复网络状态，请手动切换。建议在关机前关闭免流模式，否则下次开机后可能因为校园网认证失败无法上网，还得关上免流再登录认证*

任何免流方式都是功能大于体验

## FAQ

1. 免流模式真的能实现免流吗？

   - 使用 ipv6 不计费，具体原理 [ich.cc.cd](https://ich.cc.cd/)

2. 两个免流模式有什么区别？

   - `Warp 免流模式`：网速快，延迟较低，steam和wegame可下载。需要安装 Cloudflare WARP 客户端，少数情况无法连接
   - `DNS64 免流模式`：原生 ipv6 直连表现良好，但原本不支持 ipv6 的网站较慢，少数应用无法使用

3. 无法使用Warp免流

   - Warp 确实偶尔连不上，稍后再试
   - 确认 Cloudflare WARP 客户端已安装，且在 PATH 中可访问（首次安装重启）
   - 更换一个 ipv6 DNS，比如 Cloudflare 或阿里的
   - 类似 VMware、 Tailscale 和蓝牙的虚拟网卡可能干扰 Warp 的 DNS 连接，考虑禁用这些虚拟网卡

4. 免流模式不能访问校园网内网（例如校园网登录页）

   - 免流模式为仅 ipv6，而校园网内网资源仅支持 ipv4，如需访问校内资源请先关闭免流

5. 实时流量监控

   - 推荐 [Sniffnet](https://sniffnet.net/)

6. 反馈 Bug & 建议

   - 欢迎提 issue。或者先问问你的 ai 朋友

## 开发

**构建二进制文件**

```bash
cd BKNetwork
go build -o bknetwork.exe ./cmd/bknetwork
```

在 Windows 上构建并打包为服务或分发给别的机器时，建议在与目标平台相同的环境中构建（比如使用带有相同 GOOS/GOARCH 的交叉编译或在目标 Windows 主机上构建）

如果要连同最新前端一起发布，请使用仓库里的发布脚本，它会先同步 `web/` 再构建可分发目录和 zip 压缩包：

```bash
cd BKNetwork
.\scripts\build-release.ps1
```

`release/` 目录里会包含 `bknetwork.exe` 和最新的 `web/`，程序运行时会自动加载同步后的前端页面

```bash
# 在 Linux/macOS 环境交叉编译为 Windows amd64
GOOS=windows GOARCH=amd64 go build -o bknetwork.exe ./cmd/bknetwork
```

**以服务方式安装（Windows）**

```bash
# 以管理员身份打开 PowerShell
.\bknetwork.exe install
.\bknetwork.exe start
```

改变网络绑定或控制 `warp-cli` 的命令需要以管理员权限执行，接口会在权限不足或命令不可用时返回错误信息并通过 WebSocket 发布事件

**常见问题与排查**

- 首次打开页面状态加载慢：服务在采集网络快照时会调用若干 PowerShell 和外部命令（如 `warp-cli`），可能耗时。建议在疑难排查时直接在服务器主机上运行 `go run ./cmd/bknetwork` 并观察输出日志
- `warp-cli not found`：如果未安装 Cloudflare WARP 客户端，`/api/v1/warp` 会返回错误并在日志中给出提示。安装后确保 `warp-cli` 在 PATH 中可访问
- 权限不足：修改网卡绑定等操作需要管理员权限，若在非管理员上下文运行会收到 403 或相应错误信息

**单元测试**

```bash
cd BKNetwork
go test ./...
```

## TODO

- [x] 修复浏览器新实例bug
- [x] 新版本检测
- [x] 增加DNS64
- [x] 优化反馈频率
- [x] Warp连接失败反馈->更改探测方式
- [x] “更多免流”链接
- [x] 修复dns64免流开关闪烁
- [x] 缩减后端开销
- [x] 分离css js
- [x] 低饱和度扁平化UI
- [x] 应用图标
- [ ] 退出清理进程
- [ ] 自动登录
- [ ] 纯warp-cli
- [ ] 部分warp设置


## 免责声明

本软件仅供学习和研究使用，请勿用于任何非法用途。使用本软件产生的一切后果由用户自行承担。

具体包括但不限于：

- 本软件对因使用或无法使用而导致的任何直接、间接、特殊、偶然或后果性损害不承担责任，包括但不限于数据丢失、业务中断或利润损失。
- 开发者不保证本软件适用于任何特定目的，也不保证本软件完全没有缺陷或错误。
- 用户应当遵守相关法律法规使用本软件，不得利用本软件进行任何非法或违规操作。
- 用户应在遵守校园网及相关网络工具使用条款和规定的前提下使用本软件，开发者不对用户违规使用的行为及其后果负责。
- 因使用本软件而导致的账号被限制、设备被隔离或其他损失，开发者不承担任何责任。

使用本软件即表示你已充分理解并同意本免责声明的全部条款。

## 赞赏

感谢支持！请我杯喝的QwQ

<img alt="reward" height="250" width="250" src="https://img.ich.cc.cd/file/ichblog/img/reward.jpg" />

<br />

**鸣谢**：

  Hikio, Anon