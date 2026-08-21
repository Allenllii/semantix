# TraceLab 离线评测负载加载器（RFC 骨架，Issue #263）

> 对应 Issue：#263 引入 TraceLab 真实 coding-agent trace 作离线评测负载
> 状态：**RFC 骨架**（先立给团队审，非完整落地）。

## 一句话

把 TraceLab（arXiv:2606.30560）发布的约 4,265 个真实 Claude Code / Codex 会话 trace，作为
semantix 的**离线评测负载**，替代/补充手工小评估集，给命中率与前缀节省数字外部有效性。

## 许可核对（本 PR 已完成）

- **数据许可**：TraceLab `LICENSE-DATASET.md` —— 发布的 trace 数据（`syfi_coding_trace.jsonl.gz`
  、`syfi_coding_trace.duckdb`）采用 **CC BY 4.0**。
  - 允许分享 / 改编 / **商用**；要求**署名**（credit TraceLab / SyFI Lab, University of
    Washington，link 回 https://tracelab.cs.washington.edu）并指示是否改动。
  - **结论**：可作 semantix 离线评测用途（CC BY 4.0 允许构建衍生工作），需在产物中署名。
- **源码许可**：仓库 `LICENSE` 为 Apache-2.0，仅覆盖其源码（与本仓库无关）。
- **隐私**：数据已清洗——session/round/turn/tool-call/project/user 标识符稳定伪匿名，本地路径
  /cwd/工具输入在发布前剥离。**因此转换产物不含真实内容，仅保留 turn 结构与工具调用序列**。

## 为什么结构级就够

sanitized trace 剥离了 `tools[].input` 等本地内容，仅保留 `result_chars` 等统计。这使本负载
适合标定**结构级**评测项：

- T-Slice 转移矩阵（工具名 bigram → 下一工具概率）
- 三段占比（clear-hit / grey / miss）的分布
- prefetch MinConf / 命中率的标定负载

不适合做「内容级」检索质量评测（无真实 slice 内容可比对）。

## 目录

```
scripts/tracelab/
├── README.md      # 本文档（RFC / 许可 / 用法 / 验收）
├── fetch.py       # 下载 TraceLab 子集 + 校验和（数据不入库）
├── convert.py     # TraceLab round_trace.jsonl → semantix ingest 会话 JSONL
└── sample.py      # 分层抽样：按任务类型抽 100–500 会话做可复现子集
```

## 用法（骨架）

```bash
# 1. 下载公共子集到本地（约 X MB；脚本 + 校验和入 repo，数据不入库）
python scripts/tracelab/fetch.py --out ./tracelab-data
# 2. 分层抽样子集（可复现：固定 seed）
python scripts/tracelab/sample.py --in ./tracelab-data/round_trace.jsonl \
  --out ./tracelab-data/sample.jsonl --n 200 --seed 20260821
# 3. 转换每个会话为 semantix ingest 兼容的 JSONL
python scripts/tracelab/convert.py --in ./tracelab-data/sample.jsonl --out ./tracelab-sessions/
```

生成的 `./tracelab-sessions/*.jsonl` 直接喂给 `semantix ingest`（`kernel/ingest.JSONLSource`
兼容：`role/content/tool_calls` + `type/tool` 行）。

## 验收清单（本轮骨架的后续落地项）

- [ ] `fetch.py` 校验收 TraceLab 数据（未实测字节校验和——网络 clone 超时，见「限制」）。
- [ ] `convert.py` 输出的 JSONL 通过 `kernel/ingest` 解析（待接入后用一个样例会话验证）。
- [ ] 一条命令完成「下载子集 → 转换 → 回放 → 报告」。
- [ ] 报告给出 clear-hit / grey / miss 三段占比与自有会话分布对比表。

## 限制与后续

- 本机网络对 `github.com/uw-syfi/TraceLab.git` 的完整 clone 超时（>2min）；已改用 GitHub
  contents API 核实了布局与许可，未实测完整下载字节。`fetch.py` 的校验和需在可达网络下回填。
- 此为 RFC 骨架，**不触 kernel 行为**（Spec-Exempt E 类）。
- 落地后需在产物中给 TraceLab / UW 署名（CC BY 4.0）。
