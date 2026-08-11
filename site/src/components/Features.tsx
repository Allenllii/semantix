import type { Feature } from "@/types/content";
import Reveal from "@/components/Reveal";

const features: Feature[] = [
  {
    num: "01",
    titleEn: "Semantic Slice Library",
    title: "语义切片库",
    status: "已实现",
    body: "会话 JSONL 可提取 P/T/R 切片，并按项目或用户 scope 持久化。提取和存储路径已有自动化测试。",
    code: "extract -> P/T/R slices -> scoped store",
    evidence: "https://github.com/Gnosil/semantix/blob/main/kernel/slice/slice_test.go",
    evidenceLabel: "查看切片测试",
  },
  {
    num: "02",
    titleEn: "Semantic Cache L1·L2·L3",
    title: "三级语义缓存",
    status: "部分实现",
    body: "BM25 检索和跨会话注入块已实现并有集成测试。L3 结果复用目前只有接口，真实数据命中率仍待验证。",
    code: "BM25 + injection: implemented; L3: interface",
    evidence: "https://github.com/Gnosil/semantix/blob/main/kernel/inject/inject_test.go#L245",
    evidenceLabel: "查看跨会话测试",
  },
  {
    num: "03",
    titleEn: "Kernel Scheduler",
    title: "内核调度器",
    status: "规划中",
    body: "当前仅定义调度输入、计划与 Decider 接口。intent 学习、并发分组和模型 tier 决策尚未实现。",
    code: "Decider interface: defined; implementation: pending",
    evidence: "https://github.com/Gnosil/semantix/blob/main/kernel/sched/sched.go",
    evidenceLabel: "查看调度接口",
  },
  {
    num: "04",
    titleEn: "Speculative Prefetch",
    title: "投机预取",
    status: "规划中",
    body: "当前仅定义只读 PrefetchTask 与 Prefetcher 接口。预取策略、预算控制和实际收益尚未实现或测量。",
    code: "Prefetcher interface: defined; measurements: pending",
    evidence: "https://github.com/Gnosil/semantix/blob/main/kernel/prefetch/prefetch.go",
    evidenceLabel: "查看预取接口",
  },
  {
    num: "05",
    titleEn: "Self-Evolution",
    title: "自进化引擎",
    status: "规划中",
    body: "Signal、Params 与 Engine 接口已经定义；在线 EWMA 调参与离线重训仍是占位设计，没有效果曲线。",
    code: "Engine interface: defined; tuning loop: pending",
    evidence: "https://github.com/Gnosil/semantix/blob/main/kernel/evolve/evolve.go",
    evidenceLabel: "查看进化接口",
  },
  {
    num: "06",
    titleEn: "Single Kernel, Many Harnesses",
    title: "Harness 接入",
    status: "设计目标",
    body: "通用事件、Source 与 lookup 契约已经存在；Reasonix、Claude Code 等生产级适配尚未完成兼容验证。",
    code: "generic contracts: available; adapters: unverified",
    evidence: "https://github.com/Gnosil/semantix/blob/main/docs/reports/harness-refactor-blueprint.md",
    evidenceLabel: "查看接入蓝图",
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
            Features 实现状态
          </p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight md:text-4xl">
            机制、状态与证据。
          </h2>
          <p className="mt-4 max-w-2xl text-muted-foreground">
            以下状态以 main 分支代码和测试为准。已实现能力与路线图目标分别标注，限制条件直接写在功能说明中。
          </p>
        </Reveal>

        <div className="mt-12 grid gap-4 md:grid-cols-2">
          {features.map((f, i) => (
            <Reveal key={f.num} className="min-w-0" delay={(i % 2) * 90}>
              <article className="flex h-full flex-col rounded-lg border border-border bg-white p-6 transition hover:border-accent hover:shadow-sm">
                <div className="flex items-center justify-between gap-4 font-mono text-sm">
                  <span className="font-semibold text-accent">{f.num}</span>
                  <span className="text-xs text-muted-foreground">{f.status}</span>
                </div>
                <h3 className="mt-2 text-lg font-semibold">{f.titleEn}</h3>
                <p className="text-sm text-muted-foreground">{f.title}</p>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  {f.body}
                </p>
                <pre className="mt-4 overflow-x-auto rounded-md border border-border/60 bg-[oklch(0.976_0.005_165)] p-3 font-mono text-xs text-[oklch(0.45_0.02_260)]">
                  <code>{f.code}</code>
                </pre>
                <a
                  href={f.evidence}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-4 w-fit text-sm font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
                >
                  {f.evidenceLabel} <span aria-hidden="true">↗</span>
                </a>
              </article>
            </Reveal>
          ))}
        </div>
      </div>
    </section>
  );
}
