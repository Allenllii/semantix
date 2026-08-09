import type { Feature } from "@/types/content";
import Reveal from "@/components/Reveal";

const features: Feature[] = [
  {
    num: "01",
    titleEn: "Semantic Slice Library",
    title: "语义切片库",
    body: "从历史会话提取可复用语义单元：任务模板 P、上下文块 C、工具调用模式 T、高频结果 R、记忆 M。向量索引并持久化到项目/用户双库。",
    code: "type Slice struct { ID string; Type SliceType; Scope Scope; Content []byte }",
  },
  {
    num: "02",
    titleEn: "Semantic Cache L1·L2·L3",
    title: "三级语义缓存",
    body: "L1 字节前缀缓存；L2 把跨会话稳定的切片原样注入前缀区，让语义命中变成厂商的字节命中；L3 带验证的只读结果复用。",
    code: "L2: stable-slice injection → vendor byte cache",
  },
  {
    num: "03",
    titleEn: "Kernel Scheduler",
    title: "内核调度器",
    body: "按任务 intent 联合决策：工具并发度、模型 tier、缓存注入、预取预算——从你的行为模式里学出来的调度策略。",
    code: "decide(intent) → {concurrency, tier, inject, prefetch}",
  },
  {
    num: "04",
    titleEn: "Speculative Prefetch",
    title: "投机预取",
    body: "把 LLM 流式输出的等待时间填满：预取下一轮的切片组装、embedding 计算。waste/hit 比例自我惩罚，越用越准。",
    code: "waste/hit > 3:1 → signal-source weight ↓",
  },
  {
    num: "05",
    titleEn: "Self-Evolution",
    title: "自进化引擎",
    body: "每轮回馈命中/污染/延迟/成本/成功率：在线 EWMA 调参（冻结期保护字节缓存）+ 离线重训。系统自己长出自己的最优参数。",
    code: "online EWMA + freeze-period ≥1h + offline retrain",
  },
  {
    num: "06",
    titleEn: "Single Kernel, Many Harnesses",
    title: "零侵入适配",
    body: "不改造 harness 内核：适配层接入 DeepSeek-Reasonix、Claude Code 等任意 harness，上层能力原样复用。",
    code: "adapter(harness) → kernel.emit(event)",
  },
];

export default function Features() {
  return (
    <section id="features" className="relative overflow-hidden py-24">
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 hidden lg:block">
        <div className="absolute -right-52 top-40 h-[460px] w-[460px] rounded-full bg-[oklch(0.608_0.14_165/0.06)] blur-3xl" />
      </div>
      <div className="relative wrap">
        <Reveal>
          <p className="font-mono text-sm font-medium text-accent">
            Features 特性
          </p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight md:text-4xl">
            越用越好的能力。
          </h2>
          <p className="mt-1 text-muted-foreground">
            Autonomy you can actually audit.
          </p>
          <p className="mt-4 max-w-2xl text-muted-foreground">
            现有 harness 只做会话内的字节级前缀缓存——被动、会话绑定、调度静态。Semantix
            把整个循环升级为跨会话的、主动的、自进化的闭环：每次交互都让下一次更便宜、更快。
          </p>
        </Reveal>

        <div className="mt-12 grid gap-4 md:grid-cols-2">
          {features.map((f, i) => (
            <Reveal key={f.num} delay={(i % 2) * 90}>
              <article
                className="rounded-lg border border-border bg-white p-6 transition hover:border-accent hover:shadow-sm"
              >
                <div className="font-mono text-sm font-semibold text-accent">
                  {f.num}
                </div>
                <h3 className="mt-2 text-lg font-semibold">{f.titleEn}</h3>
                <p className="text-sm text-muted-foreground">{f.title}</p>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  {f.body}
                </p>
                <pre className="mt-4 overflow-x-auto rounded-md border border-border/60 bg-[oklch(0.976_0.005_165)] p-3 font-mono text-xs text-[oklch(0.45_0.02_260)]">
                  <code>{f.code}</code>
                </pre>
              </article>
            </Reveal>
          ))}
        </div>
      </div>
    </section>
  );
}
