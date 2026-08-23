# Spec v1 — semantix_lookup 子进程测例密封化（Issue #296）

> 判级：Spec-Exempt（E 类，测试基建 + 一处等价重构，不改 kernel 行为）。
> 生产语义零变更：`semantix_lookup` 的 3s 子进程预算与 fail-soft 契约
> 逐字保留。基线为 upstream/main `0d3af89`（含 #281 / #279 合入）。

## 1. 问题

`harness/tool.TestSemantixLookupSubprocess` 断言的是 **argv 拼装**，但它的
成败取决于三个测例并未控制的宿主环境变量：进程调度延迟、控制台代码页、
以及 PATH 上是否存在真实 `semantix` 二进制。三条耦合链各自都能让断言翻车，
且症状互不相同——这是它在 issue 里被记成「flaky」、在本地又表现为「确定性
失败」的原因。

### 1.1 真源审计（2026-08-23，worktree `fix/issue-296-lookup-subprocess-flaky`）

| 耦合链 | 证据 | 现状 |
|---|---|---|
| A. 时钟 ↔ 生产预算 | issue #296 原始报告：全仓 `-race` 下 `argv mismatch: got ""`，耗时恰为 3.00s | 已由 `2a3324c` 的 `semantixLookup{timeout:}` 注入缓解 |
| B. 控制台代码页 | 本地 `go test ./harness/tool/ -run TestSemantixLookup -count=3`：**3/3 失败**，`got "lookup --query \"\xd0\xde\xb8\xb4 go \xb2\xe2\xca\xd4\" ..."` | **活跃缺陷**，`2a3324c` 顺手加的 `.bat` shim 引入 |
| C. 宿主 PATH | `command -v semantix` → `/c/Users/liwen/go/bin/semantix` | **活跃缺陷**，`FailsSoft` / `RequiresQuery` 正在执行开发机真实内核 |

### 1.2 三条链的机理

**A（时钟）**：`Execute` 超时后 fail-soft 返回 `("", nil)`（设计行为，由
`TestSemantixLookupFailsSoft` 锚定）。测例向假内核发起的是一次**冷启动
exec**——脚本刚写入临时目录、页缓存未命中、二进制未被 AV 扫描过。该延迟
无界且随机器负载变化：本机实测首次执行新写入的可执行文件耗时 **790ms**，
其后同一文件降到 **40–75ms**（Windows Defender 首扫）。全仓 `-race` 把 CPU
打满时，这个尖峰越过 3s 预算，`Execute` 静默降级，断言拿到空串。

结论：**冷启动 exec 的延迟不可预算化，所以它永远不该被拿去和生产预算比较。**

**B（代码页）**：`.bat` shim 走 `cmd.exe`。Go 以 UTF-16 正确传入 argv，但
`echo %*` 按控制台 OEM 代码页写 stdout。本机 CP936 下「修复」出栈即
`D0 DE B8 B4`（GBK），Go 按 UTF-8 读回即乱码。同时 `%*` 会保留原始引号，
迫使断言维护两份 `want`。两者都是 `cmd.exe` 的产物，与被测代码无关。

**C（PATH）**：`TestSemantixLookupFailsSoft` 的注释写着「No semantix binary
in this environment」，但它只是**假设**，没有**保证**。开发机上真实
`semantix` 在 PATH 上，该测例每次都在跑真内核，仅因其恰好非零退出才通过；
一旦本机存在可用 slice store，`out != ""`，测例确定性失败。`RequiresQuery`
同理，且会为此付出最多 3s 墙钟。

### 1.3 统一根因

假内核 fixture 不是密封的：它借用宿主的 shell、代码页、PATH 与时钟，而不是
自己提供。逐条打补丁（放宽超时、`chcp 65001`、两份 `want`）只是把耦合搬家。

## 2. 设计

### 2.1 自执行假内核（self-exec fake）

测试二进制把**自己**复制成 `semantix`（Windows 为 `semantix.exe`）放进
`t.TempDir()`，置于 PATH 首位；再用环境变量把子进程切到假内核模式。

```
TestMain  ── 若 SEMANTIX_TEST_FAKE_KERNEL 非空 → 按模式扮演内核并 os.Exit
          └─ 否则 → m.Run()
```

模式取值：

| 值 | 行为 | 服务的测例 |
|---|---|---|
| `argv` | `fmt.Println(strings.Join(os.Args[1:], " "))`，exit 0 | argv 断言 |
| `fail` | exit 1（不写 stdout） | 「内核损坏」→ fail-soft |
| `hang` | 阻塞直到被 kill | 「内核超时」→ fail-soft（预算注入回归） |

这一步同时消掉 B 的两个症状：argv 由 Go 直接以 UTF-8 打印，不经任何 shell，
因此**所有平台共用同一份 `want`**，且不再依赖 `/bin/sh` 存在。

**为何是复制而非硬链接**：`os.Link` 在 Windows 上给正在运行的测试二进制创建
另一个名字，该镜像文件被内核锁定，`t.TempDir()` 清理时 `unlinkat ... Access
is denied`（已实测）。复制得到独立文件，子进程退出后可正常删除。成本实测
3.2 MB / ~4 ms，Linux CI 无 AV 首扫开销。

### 2.2 预算注入保留，并把生产 3s 钉死

`2a3324c` 的 `semantixLookup{timeout:}` 注入是对的，保留。但当前没有任何测例
锚定「零值 = 3s」，未来任何一次「顺手调一下超时」都能静默改掉 fail-soft 契约。

把预算解析从 `Execute` 里提出来（等价重构，无行为变化）：

```go
// budget resolves the subprocess deadline. The 3s default is part of the
// fail-soft contract: overrun degrades to an empty result, never an error.
func (s semantixLookup) budget() time.Duration {
	if s.timeout <= 0 {
		return 3 * time.Second
	}
	return s.timeout
}
```

配套测例直接断言 `semantixLookup{}.budget() == 3*time.Second`——零墙钟成本，
且是唯一一处生产常量的看守。

### 2.3 PATH 密封

所有会走到 `exec` 的测例都必须自带 PATH，不得沿用宿主的：

- 需要假内核的 → PATH 首位指向放着假内核的 `t.TempDir()`；
- 需要「内核不存在」的（`FailsSoft`、`RequiresQuery`）→ PATH 设为一个**空**
  `t.TempDir()`。`exec.LookPath` 立即失败，语义从「假设没有」变成「保证没
  有」，且墙钟从最多 3s 降到微秒级。

注：`t.Setenv` 与 `t.Parallel` 互斥。包 `harness/tool` 内无 `t.Parallel()`
（已核），本 spec 也不引入。

## 3. 断言口径

| 测例 | 断言 |
|---|---|
| `TestSemantixLookupBudget` | 零值 → `3s`；注入值 → 原样返回 |
| `TestSemantixLookupSubprocess` | 假内核 `argv` 模式，输出恰为 `lookup --query 修复 go 测试 --limit 7 --json\n`（**单一 want，跨平台**） |
| `TestSemantixLookupFailsSoft` | 空 PATH → `("", nil)`；假内核 `fail` 模式 → `("", nil)` |
| `TestSemantixLookupTimeoutDegrades` | 假内核 `hang` 模式 + 注入 200ms → `("", nil)`，且整体墙钟 < 3s（证明用的是注入预算而非生产预算） |
| `TestSemantixLookupRequiresQuery` | 空 PATH；缺 query 报错、limit 越界被夹取不报错 |

`TestSemantixLookupSubprocess` 注入 1 分钟预算：该测例断言 argv，不断言调度
时延（§1.2 A）。

## 4. 非目标

- 不改 `Execute` 的 fail-soft 语义、不改 3s 生产默认值、不改 `Schema()`。
- 不动 CI 配置（`go test ./... -race` 保持原样）。
- 不清理仓库内其它测例的同类耦合——超出 #296 范围，如有另开 issue。

## 5. 任务拆解（TDD，逐任务一次 commit）

**T1 — 钉住生产预算**
1. 写 `TestSemantixLookupBudget`，跑，预期编译失败（无 `budget`）。
2. 提取 `budget()`，`Execute` 改调它。
3. 跑测例，预期 PASS。commit。

**T2 — 自执行假内核 fixture**
1. 新增 `harness/tool/fakekernel_test.go`：`TestMain` + 模式分发 + `fakeKernelPATH(t, mode)` / `emptyPATH(t)` 助手。
2. `go vet ./harness/tool/` + 跑全包，预期既有测例不受影响。commit。

**T3 — argv 测例切到假内核**
1. `TestSemantixLookupSubprocess` 改用 `fakeKernelPATH(t, "argv")`，删掉 `.bat`
   分支与第二份 `want`。
2. 跑 `-count=3`，预期由当前的 3/3 FAIL 变 3/3 PASS。commit。

**T4 — 密封 PATH + 补 fail-soft 覆盖**
1. `FailsSoft` 拆为「内核缺失」（空 PATH）与「内核损坏」（`fail` 模式）两个子测例。
2. `RequiresQuery` 加空 PATH。
3. 新增 `TestSemantixLookupTimeoutDegrades`（`hang` 模式 + 200ms 注入）。
4. 跑全包 `-count=3`，预期全绿。commit。

**T5 — 验收**
1. `go vet ./...`
2. `go test ./harness/tool/... -count=5`
3. `go build ./...`
4. spec 文档 commit（本文件），开 PR。

## 6. 验收标准

- 本地 Windows：`go test ./harness/tool/ -run TestSemantixLookup -count=5` 全绿
  （修复前 5/5 失败）。
- 该文件内所有测例不再读取宿主 PATH、不再依赖 `/bin/sh` 或 `cmd.exe`、
  不再有平台分支的 `want`。
- `semantixLookup{}.budget()` 仍为 3s，且有测例看守。
- `go vet ./...` 与 `go build ./...` 干净。
- 已知残余：`-race` 需要 cgo，本机无 C 编译器，`-race` 全仓验证由 CI
  （ubuntu-latest）承担。
