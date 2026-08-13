export const faqItems = [
  {
    question: "Semantix 是什么？",
    answer:
      "Semantix 是一个 Go 实现的开源 Agent memory kernel。它架在 agent harness 与底层资源之间；当前可核验能力包括语义切片提取、BM25/混合检索和稳定注入，调度、预取与参数反馈仍处于接口或实验阶段。",
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
    question: "参数反馈目前实现到什么程度？",
    answer:
      "仓库实现了 EWMA、冻结窗口与参数状态持久化，用于验证反馈调参路径。离线重训、真实会话闭环和成本或性能收益尚未经过独立生产评测。",
  },
  {
    question: "如何参与 Semantix 的开发？",
    answer:
      "在 github.com/Gnosil/semantix 提 issue 或开 PR。提交时请附运行环境与 go vet、go test 的真实结果；若存在平台差异或失败项，应一并记录，而不是笼统写成全绿。",
  },
] as const;
