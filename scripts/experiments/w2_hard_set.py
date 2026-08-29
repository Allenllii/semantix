#!/usr/bin/env python3
"""Hard paraphrase set (W2 calibration round 2): rephrase the 10 gateway-m1
questions with MINIMAL lexical overlap — synonyms replaced, domain jargon
swapped for descriptions. This is where BM25 must fail and real embeddings
must win; if both fail, zone thresholds need recalibration, not retrieval.
"""

import json
import pathlib

HARD = [
    " goroutine 退出前注册的延迟函数什么时候跑？程序崩溃时它们和 recover 的互动是怎样的",  # noqa: leading space intentional trimming below
    "为什么很多人说 CPython 里线程没法真正并行跑字节码？遇到 CPU 密集任务时替代方案是什么？",
    "数据库里按标签搜索文章该建普通有序索引还是倒排类索引？多列一起查时的最佳实践是什么？",
    "发布一个后端服务时怎么让容器镜像从上 GB 缩到几十 MB？给个编译型语言的分阶段构建样例",
    "把个人开发线的提交压平到主线上，和直接合入保留分叉历史，各自的风险点在哪？什么时候改写历史会坑队友？",
    "浏览器怎么判断一个资源还能用本地副本？强缓存与协商缓存的各种响应头应如何配合使用？",
    "内存快照和追加日志两种落盘方式各牺牲什么换什么？两者结合的方案在重启回放时按什么顺序应用？",
    "容器编排平台上，探活失败重启容器与未就绪摘除流量，这两种健康检查配置写反了会发生什么？",
    "写正则时量词默认尽量多吃字符，加问号变保守——为什么嵌套量词会让匹配引擎卡死？怎么绕开？",
    "建立加密连接时浏览器如何逐级确认服务器身份？证书链少了一环为什么某些客户端会拒绝连接？",
]

out = pathlib.Path("/tmp/tracelab")
with (out / "gwm1-hard.jsonl").open("w") as f:
    for q in HARD:
        q = q.strip()
        f.write(json.dumps({"role": "user", "content": q}, ensure_ascii=False) + "\n")
        f.write(json.dumps({"role": "assistant", "content": "(answer)"}, ensure_ascii=False) + "\n")
print("hard set written")
