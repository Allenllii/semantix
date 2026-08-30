# SWE-bench 双臂 pilot 报告（10 实例 × 2 臂）

> 日期：2026-08-30/31。对应计划：`docs/specs/swebench-efficiency-research-plan.md` W1（离线部分转真实）。
> 这是 semantix 第一份**真实 agent 运行 + 官方 SWE-bench harness 评测**的双臂数据，取代此前一切合成回放数字。

## 1. 实验设置

- **基准**：SWE-bench Verified，按 repo 分层选 2 × 5 实例（django × 5、sympy × 5，各 repo 内取 problem_statement 最短的实例控制单实例成本）。
- **模型**：deepseek-v4-flash（与 v28 单实例先例一致），agent 为 `cmd/semantix-agent`（yolo 权限、80 步上限、单实例 1200s 看门狗）。
- **OFF 臂**：`[semantix] enabled=false`，每实例全新会话。
- **ON 臂**：`enabled=true, inject=true, budget=4096`；实例按序运行，第 N 个实例开始前把前 N-1 个实例的会话镜像 extract 进该 repo 的 Project-scope 库（`.semantix/project.db`，累积注入已验证生效：L2 SliceInject 事件 + 库增长确认）。
- **评测**：官方 `swebench.harness.run_evaluation`（v5.0.2，Docker，x86_64 镜像经 Rosetta 转译；本地补丁：pull 调用显式 `platform="linux/amd64"`）。
- **成本**：双臂合计 $0.15 API 费用（真实计费，agent 自报）。

## 2. 结果

### 质量（官方 harness，唯一权威口径）

| 臂 | resolved | unresolved | infra failure |
|---|---|---|---|
| OFF | 9/10 | 1（sympy-20916） | 0 |
| **ON** | **10/10** | 0 | 0 |

**pass-rate 非劣成立**（ON 甚至 +1，单实例差异无统计意义，但至少证明注入未伤害正确性）。

### 效率（agent 自报 usage，真实计费）

| 指标 | OFF | ON | Δ |
|---|---|---|---|
| 输入 tokens 总计 | 4,353,870 | 6,932,300 | **+59.2%** |
| API 成本总计 | $0.0544 | $0.0980 | **+80.2%** |
| 墙钟（剔除机器睡眠污染） | 1085s | 1032s | ≈ 持平 |
| 前缀缓存命中率 | 97.3% | 95.0% | −2.3pt |
| 非空 patch | 10/10 | 10/10 | = |

单实例方差极大（这本身是个发现）：14373 ON 比 OFF 省 64% token（77K vs 171K），15987 省 72%；但 16819 ON 多花 161%（3.19M vs 1.22M）。

## 3. 诚实结论

1. **全链路首次真实打通**：选实例 → repo checkout → agent 双臂运行 → 跨实例切片库累积 → L2 注入进真实 LLM 请求 → 官方 Docker 评测。管线、看门狗、镜像去重等工程问题全部解决并沉淀在 `scripts/experiments/swe_pilot/run_arm.sh`。
2. **本 pilot 的 ON 臂在 token/成本上是负收益（+80%）**。这符合研究计划 W0 的预测：当前抽取器产出的 turn 级 Prompt 切片和工具名级 T-Slice，对同 repo 的后续实例是弱信号——注入字节本身有成本，检索到的切片又可能引导 agent 多探索。**切片质量问题现在是唯一的主要矛盾**，且有两个现成的修复方向：
   - W4 已落地的 `gc --consolidate-context`（近重复合并成 repo 概览卡）尚未接入 agent 库的日常维护路径；
   - 注入门控过松：灰区 audit 模式 + 全类型注入应改为「仅 zone.Hit 且仅 C/M 类型注入」，#259 的分型阈值正好提供机制。
3. **质量非劣 + 成本上升**说明系统当前处于「正确性优先」设计原则的正确一侧（fail-open 没有伤害 pass-rate），但「等效最省」的目标尚未达成——瓶颈不在机制在内容。
4. 协同因素：本 pilot 的库只累积了 0–9 个同 repo 实例（冷启动),而注入对每个 turn 都发生。真实使用场景中库会更厚；但反向结论同样成立——**库薄时注入应有更高的准入门槛**。

## 4. 下一步（按杠杆排序）

1. 把注入准入改为：仅 zone.Hit + per-type 阈值（T/R 缺省不注入，#259/#268 机制已就位），重跑本 pilot 对比。
2. 接入 Context consolidation：实例完成后跑 `gc --consolidate-context`，让后续实例注入「repo 概览卡」而非原始 turn 切片。
3. 扩到 20 实例 × 3 重复（消除单次方差），预算 < $5。
4. 评测镜像磁盘占用 ~40GB，复跑前无需重拉（cache 已在本地 Docker）。

## 5. 复现

```bash
# 数据与实例选择
/tmp/femb/bin/python - <<'EOF'  # 见 git 历史: 选取 django×5 + sympy×5 最短 problem_statement
EOF
# 双臂运行（需 ~/.semantix/.env 提供 DEEPSEEK_API_KEY）
MAX_STEPS=80 AGENT_TIMEOUT_SECS=1200 bash scripts/experiments/swe_pilot/run_arm.sh off /tmp/semantix-run/pilot-off
MAX_STEPS=80 AGENT_TIMEOUT_SECS=1200 bash scripts/experiments/swe_pilot/run_arm.sh on  /tmp/semantix-run/pilot-on
# 官方评测（arm64 宿主机需 platform 补丁，见下）
/tmp/femb/bin/python -m swebench.harness.run_evaluation --dataset_name SWE-bench/SWE-bench --split test \
  --predictions_path /tmp/semantix-run/pilot-on/predictions.jsonl --max_workers 1 -id swe_pilot_on
```

已知的三个坑（脚本已内置修复）：`--permission-mode yolo`（acceptEdits 会拒掉全部 shell 命令）；LLM 连接偶发挂死需独立杀手进程看门狗；bridge 会话 label 是模型名常量、镜像文件需每轮运行前清空后搬运。
