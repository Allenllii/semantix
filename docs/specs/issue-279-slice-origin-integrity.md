# Spec v1 — 切片来源/完整性标签:低完整性来源不得直通注入(Issue #279)

> 判级:Spec-Required。本 spec 细化 Issue #279「切片加来源/完整性标签」:
> SliceMeta 增加 Origin 来源标签,注入/L3 按来源档位限权——import 档与
> 未标注切片不得直通注入与 L3 Hit(强制 Grey 走 judge),`semantix trust`
> 显式升档并记审计行。基线:upstream/main 最新。待团队评审。

## 1. 现状审计(真源,2026-08-23,explore 子代理全仓只读)

| 事实 | 证据 |
|---|---|
| `SliceMeta` 14 字段,来源字段是**描述性**的(`SourceSession/ProjectSlug`),无信任/完整性语义;`L3Safe` 是人工 opt-in 且仅在 Deps 为空时被查询 | `kernel/slice/slice.go:133-194`(L3Safe 157,注释 152-156) |
| 写入通道共 5 处,产物在检索/注入/L3 中**完全同权**:ingest 自动提取、prefetch 自动产出、extract CLI、gateway ingestSession、verify 训练切片 | `kernel/ingest/ingest.go:238-261`;`kernel/prefetch/runner.go:70-82`(`SourceSession:"prefetch:"+t.Key`);`cmd/semantix/extract.go:54-91,130`;`gateway/gateway.go:451-486`;`cmd/semantix/verify.go:529-539` |
| **import 命令不存在**:`kernel/slice/maintenance.go:100` 有 `Import()` 函数但无 CLI 包装;命令树 13 命令无 import/trust;`gc.go:23` 注释是死引用 | `cmd/semantix/main.go:122-187`;`kernel/slice/maintenance.go:70,100` |
| **trust 命令不存在**(harness 侧 trust 是 agent 内 bash 只读审批,与切片库无关) | `cmd/semantix` 命令树;`harness/control/controller.go:6096` |
| 注入限权点唯一:`inject.Build` 的 zone 过滤(非 Hit 跳过)——无来源维度 | `kernel/inject/inject.go:104-166`(zone 过滤 131-140) |
| L3 直通:`DecideL3` 的 `zone.Hit` 分支(232-244),lexical gate(#260)降级 Grey 走 judge;Grey 分支 245-251 `judgeGrey`(nil judge → 保守拒绝) | `kernel/cache/l3.go:161-282,385-436` |
| **审计 JSONL 无实现**:事件总线纯内存;Security §8 要求「切片入库(来源+净化动作)」审计日志当前无落盘通道 | `kernel/event/bus.go`(SyncBus 无持久化);`docs/Security-安全设计.md:178-183`;`docs/events.md:85`(SliceHit/Inject/Reject 零生产者) |
| 落盘格式:journal 行 + sliceDTO;SliceMeta 无 tag 字段按大写名落盘;**老库缺失字段自然零值**(新增字段向后兼容的基础) | `kernel/slice/file_store.go:64-92,123-167` |
| 测试断言:`prefetch:` 前缀仅 `kernel/prefetch/runner_test.go:61-62` | 同上 |

## 2. 目标与非目标

### 2.1 目标

```text
SliceMeta.Origin 来源标签(5 个写入点各自填充)
  → 档位映射(可配置阈值):import/legacy < session-auto = prefetch < user-curated
  → inject:档位不足的候选不进注入块(zone 过滤后追加)
  → L3:档位不足的候选 Hit 强制降 Grey(走 judge,nil judge 保守拒绝)
  → semantix trust <id> 显式升档(记审计行 JSONL)
  → semantix import CLI 恢复(origin=import,--trust 立即升 user-curated)
  → 审计 JSONL 通道(切片入库来源标注 + 升档动作,对齐 Security §8)
```

### 2.2 非目标(列为后续)

- **内容净化**:与「ingest 净化」RFC 互补(净化改内容、标签改权限),本期不做净化;
- **加密来源证明**(MemoryGraft 缓解的完整形态):本期是单机标签 + 限权,
  不做签名/密钥;
- **跨进程信任传递**(gateway → harness 的信任上下文传播):本期限权在
  kernel 层,gateway/CLI 自动受益,harness 桥接的信任上下文后续 issue;
- **注入块的来源标注展示**(UI/回显):本期只做限权,不做展示。

## 3. 设计

### 3.1 `Origin` 来源标签(单一真源 `kernel/slice`)

```go
// Origin 是切片的来源/信任标签(Issue #279):写入点填充,检索/注入/L3
// 按档位限权。空字符串 = 未标注(legacy),按最低档处理(fail-closed)。
type Origin string

const (
    OriginSessionAuto Origin = "session-auto" // 会话自动提取(ingest/gateway)
    OriginPrefetch    Origin = "prefetch"     // 投机预取自动产出
    OriginImport      Origin = "import"       // 外部导入(最开放通道)
    OriginUserCurated Origin = "user-curated" // 用户显式策展(trust 升档后)
)
```

- `SliceMeta` 新增 `Origin Origin \`json:"origin,omitempty"\``——wire-stable:
  空 = 未标注(老库/老二进制互读,缺失自然零值);
- **档位数值**(内部,不落盘):`import=1, legacy(空)=1, session-auto=2,
  prefetch=2, user-curated=3`;`Level() int` 方法(空 → 1);
- 档位映射可配置(issue 建议 2):`[slice] min_inject_origin`(默认
  `session-auto`,即档位 2)——低于该档的切片不注入不直通。

### 3.2 写入点填充

| 写入点 | Origin | 位置 |
|---|---|---|
| ingest 自动提取 | `session-auto` | `kernel/ingest/ingest.go` Extract 调用处 |
| gateway ingestSession | `session-auto` | `gateway/gateway.go:451-486` |
| prefetch 产出 | `prefetch` | `kernel/prefetch/runner.go:75` |
| extract CLI | `user-curated` | `cmd/semantix/extract.go`(用户显式执行 = 策展动作) |
| import CLI(本期恢复) | `import`(默认)/ `user-curated`(`--trust`) | `cmd/semantix/import.go` 包装 `maintenance.Import` |
| verify 训练切片 | `session-auto` | `cmd/semantix/verify.go:534`(仅 verify.db 临时库,不参与注入) |

### 3.3 限权点(复用现有 zone 机制,不发明新机制)

- **inject**(`kernel/inject/inject.go` Build):zone 过滤(131-140)后追加
  来源过滤——`s.Meta.Origin.Level() < minInjectOrigin` 的候选 `dropped++`
  跳过(统计进现有 dropped 计数,观测不新增机制);
- **L3**(`kernel/cache/l3.go` DecideL3):候选 `zone.Hit` 且
  `Origin.Level() < minInjectOrigin` → **降级为 Grey** 走 `judgeGrey`
  (nil judge → 保守拒绝,与 lexical gate 降级同路径);Grey/Miss 分支不变;
- 限权阈值注入:`L3Decider` 与 `inject.Injector` 各增 `MinOrigin slice.Origin`
  字段(零值 = 最低档,即不过滤——kernel 层默认不收紧,由 gateway/CLI
  配置层决定阈值,避免内核默认行为突变)。

> 设计决策:阈值默认在**配置层**为 `session-auto`(gateway `[slice]`
> 配置 + CLI 默认),kernel 层零值不过滤——这样直接调用 kernel 的测试
> 与嵌入方不受影响,生产路径(经 gateway/CLI)默认 fail-closed。

### 3.4 `semantix trust` 命令(升档 + 审计)

```
semantix trust <slice-id> [--origin user-curated|session-auto|prefetch]
```

- 仅允许**升档**(目标档 > 当前档,不允许降档——降档无安全意义且防误操作);
- 目标默认 `user-curated`;
- 写审计行(§3.5);`--json` 信封输出;exit 0/1/2(U19 契约)。

### 3.5 审计 JSONL(对齐 Security §8③)

- 新审计文件 `.semantix/audit.jsonl`(0600,追加写,容忍坏行——与
  usage.jsonl 同模式;路径经 `--audit-db` 可覆盖,默认
  `store.db` 同目录下 `.semantix/audit.jsonl`);
- 记录动作(本期):`slice_origin`(写入点标注,含来源)、
  `slice_trust`(升档:slice_id/from/to);
- 行格式:`{"at":<unix>,"action":"slice_trust","slice_id":"...",
  "from_origin":"import","to_origin":"user-curated"}`;
- 审计写入 best-effort(失败不阻塞主路径,与 recordUsage 同纪律)。

### 3.6 `semantix import` CLI 恢复

```
semantix import --input <jsonl> [--db <path>] [--trust] [--audit-db <path>]
```

- 包装 `kernel/slice/maintenance.Import`;默认 `origin=import`;
  `--trust` 立即升 `user-curated`(import 后直接 trust,一次动作);
- 老库兼容:import 的文件若无 Origin 字段,落库时统一标 `import`(读取
  通道不可信,不能继承文件内声明)。

## 4. 兼容性与迁移影响(必须知悉)

- **存量库行为变化(有意收紧)**:升级后,库内**未标注(legacy)切片按
  import 档处理 → 不再注入、不再 L3 直通**(fail-closed,issue 验收要求)。
  受影响面:注入块缩小、L3 命中率下降;补救:`semantix trust <id>`
  逐条升档,或配置 `[slice] min_inject_origin = "import"` 整体放行
  (运维显式声明"老库可信");
- 新增字段 additive,老二进制可读新库、新二进制可读老库(journal 缺失
  字段零值);
- `SliceMeta.Origin` 不进 slice ID 计算(不改变既有切片 ID);
- 事件/usage 契约无变化。

## 5. 配置契约

```toml
[slice]
min_inject_origin = "session-auto"  # 注入/L3 直通最低档:import|session-auto|prefetch|user-curated
                                    # (默认 session-auto:import 与 legacy 被排除)
```

- gateway `Config` 新增 `[slice]` 段(`MinInjectOrigin string`),
  `validate()` 校验枚举;`New()` 注入 `L3Decider.MinOrigin` 与
  `inject.Injector.MinOrigin`;
- CLI 默认:`semantix lookup/inject` 等注入消费方默认
  `min_inject_origin=session-auto`(与 gateway 一致,经配置键下发);
- kernel 层零值 = 不过滤(嵌入方显式选择)。

## 6. 验收标准

- [ ] **c1 标签落库**:5 个写入点(ingest/gateway-ingest/prefetch/extract/
  import)产物分别带 `session-auto/prefetch/user-curated/import` Origin,
  json 落盘可见;
- [ ] **c2 注入限权**:`min_inject_origin=session-auto` 时,import/legacy
  切片不出现在注入块(单测 + e2e);`user-curated/session-auto/prefetch`
  照常注入;
- [ ] **c3 L3 限权**:import/legacy 切片 zone.Hit 时降 Grey 走 judge,
  nil judge 保守拒绝(不直通);session-auto 以上照常直通;
- [ ] **c4 legacy fail-closed**:老库(无 Origin 字段)切片按最低档处理,
  不注入不直通(单测);配置 `min_inject_origin=import` 可整体放行;
- [ ] **c5 trust 升档**:`semantix trust <id>` 升 user-curated 后切片
  恢复注入/L3 直通;降档被拒;审计行写入 audit.jsonl;
- [ ] **c6 import CLI**:`semantix import --input` 落库标 `import`,
  `--trust` 标 `user-curated`;审计行写入;
- [ ] **c7 审计通道**:audit.jsonl 追加写、坏行容忍、0600 权限;切片入库
  来源标注与升档动作均可查询;
- [ ] **c8 兼容**:新字段 additive,老库可读;Slice ID 不变;vet/测试全绿
  (新增测试覆盖 c1-c7;Windows 平台既有失败按惯例对照基线)。

## 7. 测试计划(按风险放置)

| 层 | 测试 | 覆盖 |
|---|---|---|
| kernel/slice | Origin Level 映射(空→1)、json round-trip(空/各档)、legacy 零值 | c1/c4/c8 |
| kernel/inject | 档位过滤(import 不进块、session-auto 进)、MinOrigin 零值不过滤 | c2/c4 |
| kernel/cache | L3 降级(import Hit→Grey→nil judge 拒绝)、MinOrigin 零值行为不变 | c3/c4 |
| cmd/semantix | trust 升档/降档拒绝/审计行、import CLI(默认 import/--trust)、配置键校验 | c5/c6 |
| gateway | [slice] 配置接线与 validate;e2e:import 切片不注入 | c2/c6 |
| 审计 | audit.jsonl 追加/坏行/权限 | c7 |

## 8. 实施顺序(建议)

1. `kernel/slice`:Origin 类型 + Level + SliceMeta 字段 + 单测;
2. 5 个写入点填充 Origin(ingest/gateway/prefetch/extract/verify);
3. kernel 限权:`inject.Build` 过滤 + `L3Decider` 降级(MinOrigin 字段);
4. 审计通道 `kernel/audit`(或 cmd 层 helper):audit.jsonl 写入;
5. `semantix trust` + `semantix import` CLI;
6. gateway/CLI 配置接线与 e2e;
7. 文档(QUICKSTART/Security §8 关联/README)+ 验收。

## 9. 参考

- FIDES: https://arxiv.org/abs/2505.23643 · TMA-NM: https://arxiv.org/abs/2606.24322
- MemoryGraft: https://arxiv.org/abs/2512.16962 · AgentPoison: https://arxiv.org/abs/2407.12784
- `docs/Security-安全设计.md` §8(审计要求)、`kernel/slice/slice.go`、
  `kernel/inject/inject.go`、`kernel/cache/l3.go`、`kernel/slice/maintenance.go`(当前真源)
- Issue #204(journal schema 演进先例)、#260(词法门降级先例)
