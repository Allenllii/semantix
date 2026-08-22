# Issue #348 — Windows 长时间闲置后 TUI 鼠标协议泄漏到父 Shell

## 现象

Windows Terminal/ConPTY 中启动 `semantix-agent`，长时间闲置、休眠或切走窗口后返回，父 PowerShell 提示符可能已经出现。此后点击或滚轮会向提示符写入类似 `ESC[<64;121;14M` 的文本。这是 SGR 鼠标报告，不是字符编码损坏。

## 根因

TUI 使用 `tea.MouseModeCellMotion`，并在 focus、resize 和 turn settle 时重发 `1002 + 1006` 鼠标模式。终端恢复依赖 Bubble Tea renderer 正常 shutdown；应用层只在 Ctrl+Z 前显式发送完整 `resetMouseTracking`。当 `Program.Run` 异常返回、panic 展开，或 shutdown 与延迟 `mouseReenableMsg` 竞争时，父 Shell 可能恢复输入而终端仍保持鼠标上报。

诊断日志还会在 idle 状态每秒写 heartbeat，约数小时耗尽 4 MiB 上限，导致真正退出阶段没有证据。

## 验收条件

- [ ] `Program.Run` 的正常返回、错误返回和 panic 路径均最终写入完整鼠标 reset。
- [ ] TUI 收到 shutdown 后不再发送 mouse enable 序列。
- [ ] idle watchdog 仍不升级动作，但 heartbeat 至多每分钟写一次。
- [ ] 回归测试覆盖上述行为，且 `go test ./harness/cli` 通过。

## 非目标

- 不改变鼠标选择、滚轮或滚动条行为。
- 不处理操作系统强制终止后无法执行用户态清理的情况。

