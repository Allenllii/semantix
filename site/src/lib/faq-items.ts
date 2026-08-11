export const faqItems = [
  {
    question: "Semantix 是什么？",
    answer:
      "Semantix 是一个自进化的 Agent Kernel 层，Go 实现、开源。它架在 agent harness 与底层资源之间，通过语义切片库、三级语义缓存、内核调度器和投机预取，让每次交互都更便宜、更快。",
  },
  {
    question: "Semantix 解决什么问题？",
    answer:
      "三个核心问题：跨会话的相似工作无法复用，每次从零开始；调度是静态规则，不随任务类型和用户习惯自适应；LLM 流式输出的等待时间被浪费。",
  },
  {
    question: "L1 / L2 / L3 三级语义缓存分别是什么？",
    answer:
      "L1 是厂商的字节级前缀缓存，仅在会话内被动命中；L2 把跨会话稳定的切片原样注入前缀区，让语义命中转化为厂商字节缓存命中；L3 对只读任务带文件指纹验证直接复用历史结果，验证不过则拒绝。",
  },
  {
    question: "“自进化”具体指什么？",
    answer:
      "系统每轮采集命中率、污染、延迟、成本、成功率等信号：在线用 EWMA 调整参数，参数变更后冻结期至少一小时以保护字节缓存；离线做嵌入刷新、阈值网格搜索和 T-Slice 转移矩阵重训。参数由系统自己调整，不是人工调优。",
  },
  {
    question: "如何参与 Semantix 的开发？",
    answer:
      "在 github.com/Gnosil/semantix 提 issue 或开 PR。当前 M0 阶段按工作单元推进，PR 需附 go vet 与 go test 全绿的验证结果。",
  },
] as const;
