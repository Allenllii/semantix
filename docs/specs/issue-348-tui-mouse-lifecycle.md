# Spec — Issue #348 TUI 鼠标生命周期闭环

## 1. 状态模型

`chatTUI` 新增不可逆的 `shuttingDown bool`。`tuiShutdownMsg` 首先置位并清除 pending timer 状态；`maybeReenableMouse` 在该状态下返回 nil。已排队的 focus、resize、settle 和 `mouseReenableMsg` 因此不再开启鼠标模式。

## 2. 退出兜底

用窄接口 `tuiProgramRunner { Run() (tea.Model, error) }` 包装 `p.Run()`。`runTUIProgram` 在调用前 defer 写入 `resetMouseTracking`，覆盖正常返回、错误返回和 panic 展开。DEC private-mode reset 幂等，和 Bubble Tea 自身清理重复发送兼容。

## 3. 诊断日志

watchdog 检查频率保持一秒；idle heartbeat 首次立即记录，之后至少间隔一分钟。状态转换和 running/booting 检测不采样。

## 4. 测试

1. fake runner 正常、错误和 panic 路径均输出 reset。
2. shutdown 后 `maybeReenableMouse` 返回 nil 并清除 pending。
3. `tuiShutdownMsg` 将模型标记为 shutting down。
4. idle 连续 tick 只按一分钟间隔记录且从不升级。

## 5. 兼容性

不新增配置、CLI flag 或依赖；不承诺覆盖 `TerminateProcess`、断电等无法运行 defer 的强制终止。

