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
    body: "当前仅定义调度输入、RoundPlan 与 Decider 接口；intent 学习、并发规划和 tier 选择尚未实现。",
    code: "Decider interface: implementation pending",
  },
  {
    num: "04",
    titleEn: "Speculative Prefetch",
    title: "投机预取",
    body: "Prefetcher 当前仅定义只读任务接口；转移预测、预算控制和 waste/hit 测量仍在规划中。",
    code: "Prefetcher interface: implementation pending",
  },
  {
    num: "05",
    titleEn: "Self-Evolution",
    title: "自进化引擎",
    body: "Signal、Params 与 Engine 接口已经定义；在线 EWMA 调参与离线重训仍未实现或验证。",
    code: "Engine interface: tuning loop pending",
  },
  {
    num: "06",
    titleEn: "Single Kernel, Many Harnesses",
    title: "零侵入适配",
    body: "通用事件与适配契约已经设计；Reasonix、Claude Code 等生产级适配尚未完成兼容验证。",
    code: "generic contracts: adapters unverified",
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
            核心能力与当前进度。
          </h2>
          <p className="mt-1 text-muted-foreground">
            Autonomy you can actually audit.
          </p>
          <p className="mt-4 max-w-2xl text-muted-foreground">
            当前已实现会话切片、BM25 检索与跨会话注入；调度、预取和自进化闭环仍处于接口或规划阶段。
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
