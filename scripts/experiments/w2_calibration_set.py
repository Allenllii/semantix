#!/usr/bin/env python3
"""Build the W2 calibration set: 10 real gateway-m1 queries (library) plus
10 paraphrases of the same 10 questions (probe session). Paraphrases are
hand-written rephrasings that preserve intent while changing wording — the
"same task, different bytes" case L2 injection exists for.
"""

import json
import pathlib

LIB = [
    "请解释 Go 语言中 defer 语句的执行顺序以及它与 panic recover 的配合方式",
    "Python 的 GIL 全局解释器锁对多线程性能有什么影响，什么场景下应该改用多进程",
    "PostgreSQL 里 B-tree 索引和 GIN 索引的适用场景差异，什么时候该建联合索引",
    "Docker 多阶段构建如何显著减小最终镜像体积，写一个 Go 项目的完整示例",
    "git rebase 和 git merge 在协作分支上的取舍，什么情况下绝对不能 rebase",
    "HTTP 缓存里 ETag 与 Last-Modified 的协商机制差异，Cache-Control 各指令怎么组合",
    "Redis 持久化 RDB 与 AOF 的取舍，混合持久化在崩溃恢复时的具体行为",
    "Kubernetes 中 readinessProbe 与 livenessProbe 的区别，配置不当会引发什么故障",
    "正则表达式中的贪婪匹配与懒惰匹配区别，回溯灾难是怎么产生的以及如何避免",
    "TLS 握手过程中证书链验证的完整步骤，中间证书缺失会导致什么样的错误",
]

# Paraphrases: same question, different wording/order/terms.
PARA = [
    "讲一讲 Go 的 defer 是按什么顺序执行的，和 panic、recover 之间如何协作",
    "GIL 这把全局锁对 Python 多线程意味着什么？多进程在哪些情况下更合适？",
    "Postgres 的 B-tree 与 GIN 索引各自适合什么查询？复合索引应该在何时创建？",
    "怎样用 Docker 的 multi-stage build 压缩镜像大小？给出一个 Go 服务的完整 Dockerfile 例子",
    "团队协作分支里 rebase 与 merge 该怎么选？哪些情形下 rebase 是绝对禁止的？",
    "对比 HTTP 协商缓存中 ETag 和 Last-Modified 的工作方式，以及 Cache-Control 各指令的搭配规则",
    "Redis 的 RDB 快照与 AOF 日志如何权衡？混合持久化在宕机恢复时究竟做了什么？",
    "K8s 里 livenessProbe 和 readinessProbe 有什么不同？如果配错了分别会造成什么线上问题？",
    "正则里贪婪量词和懒惰量词有什么差别？灾难性回溯是怎么触发的，又该如何规避？",
    "描述 TLS 握手时证书链校验的完整流程，如果缺少中间证书会报什么错",
]

out = pathlib.Path("/tmp/tracelab")
for name, queries in (("gwm1-lib", LIB), ("gwm1-para", PARA)):
    with (out / f"{name}.jsonl").open("w") as f:
        for q in queries:
            f.write(json.dumps({"role": "user", "content": q}, ensure_ascii=False) + "\n")
            f.write(json.dumps({"role": "assistant", "content": "(answer)"}, ensure_ascii=False) + "\n")
print("calibration set written")
