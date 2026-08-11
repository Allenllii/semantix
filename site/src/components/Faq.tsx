import Reveal from "@/components/Reveal";

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
];

export default function Faq() {
  return (
    <section id="faq" className="relative overflow-hidden py-24">
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 hidden lg:block">
        <div className="absolute -left-48 bottom-16 h-[360px] w-[360px] rounded-full bg-[oklch(0.608_0.14_165/0.06)] blur-3xl" />
      </div>
      <div className="relative wrap">
        <Reveal>
          <p className="font-mono text-sm font-medium text-accent">FAQ 常见问题</p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight md:text-4xl">
            常见问题。
          </h2>
          <p className="mt-1 text-muted-foreground">
            Five questions answered in facts.
          </p>
        </Reveal>

        <div className="mt-12 max-w-3xl space-y-4">
          {faqItems.map((item, i) => (
            <Reveal key={item.question} delay={i * 60}>
              <div className="rounded-lg border border-border bg-white p-6">
                <h3 className="font-semibold">{item.question}</h3>
                <p className="mt-2 text-sm leading-7 text-muted-foreground">
                  {item.answer}
                </p>
              </div>
            </Reveal>
          ))}
        </div>
      </div>
    </section>
  );
}
