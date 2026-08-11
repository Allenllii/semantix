import Link from "next/link";
import Reveal from "@/components/Reveal";
import { siteIdentity } from "@/lib/site-identity";

type ComponentCard = {
  num: string;
  titleEn: string;
  title: string;
  status: "已实现" | "部分实现" | "接口阶段";
  responsibility: string;
  io: string;
  limitation: string;
  evidence: string;
  evidenceLabel: string;
};

const components: ComponentCard[] = [
  {
    num: "01",
    titleEn: "Semantic Slice Library",
    title: "语义切片库",
    status: "已实现",
    responsibility: "解析会话 JSONL，按 turn 边界生成 Prompt、ToolPattern 和 Result 切片，并按 scope 持久化。",
    io: "输入：Reasonix 风格会话 JSONL。输出：带内容哈希 ID、类型、scope 与来源元数据的 Slice。",
    limitation: "默认提取器暂不生成 Context 和 Memory 切片；畸形行会跳过，Prompt 与 Result 分别限制为 4 KB 和 8 KB。",
    evidence: "https://github.com/Gnosil/semantix/blob/main/kernel/slice/extractor.go",
    evidenceLabel: "查看提取器源码",
  },
  {
    num: "02",
    titleEn: "Retrieval & Injection",
    title: "检索与跨会话注入",
    status: "部分实现",
    responsibility: "使用 BM25 按 scope 检索切片，并把命中项按稳定顺序组装为可逆的注入块。",
    io: "输入：查询文本、scope 与切片库。输出：排序后的命中项或注入块；未命中时返回空结果。",
    limitation: "跨会话路径已有集成测试，但 L3 结果复用仍只有接口，真实用户命中率和生产 harness 挂载尚未验证。",
    evidence: "https://github.com/Gnosil/semantix/blob/main/kernel/inject/inject_test.go#L245",
    evidenceLabel: "查看跨会话测试",
  },
  {
    num: "03",
    titleEn: "Kernel Scheduler",
    title: "内核调度器",
    status: "接口阶段",
    responsibility: "定义工具轮次的联合决策契约，包括并发组、模型 tier、注入切片和预取目标。",
    io: "输入：RoundInput，包含 intent、工具调用、切片命中、预算和可用并发。输出：RoundPlan。",
    limitation: "仓库当前只有 Decider 接口和数据结构，没有 intent 学习、并发规划或 tier 选择实现。",
    evidence: "https://github.com/Gnosil/semantix/blob/main/kernel/sched/sched.go",
    evidenceLabel: "查看调度接口",
  },
  {
    num: "04",
    titleEn: "Speculative Prefetch",
    title: "投机预取",
    status: "接口阶段",
    responsibility: "定义基于近期工具序列规划只读预取任务的契约，并为每个任务记录类型、键和估算成本。",
    io: "输入：最近使用的工具名序列。输出：零个或多个只读 PrefetchTask。",
    limitation: "当前没有 Planner 实现、转移矩阵、预算执行或 waste/hit 基准，因此不声明延迟或成本收益。",
    evidence: "https://github.com/Gnosil/semantix/blob/main/kernel/prefetch/prefetch.go",
    evidenceLabel: "查看预取接口",
  },
];

export default function Components() {
  return (
    <section id="components" className="relative overflow-hidden bg-[oklch(0.976_0.005_165)] py-24">
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 hidden lg:block">
        <div className="absolute -left-52 bottom-24 h-[420px] w-[420px] rounded-full bg-[oklch(0.524_0.12_165/0.07)] blur-3xl" />
      </div>
      <div className="relative wrap">
        <Reveal>
          <p className="font-mono text-sm font-medium text-accent">
            Components 组件边界
          </p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight md:text-4xl">
            每个组件做什么，也说明不做什么。
          </h2>
          <p className="mt-4 max-w-2xl text-muted-foreground">
            以下说明以 main 分支代码为准。仓库目前没有发布组件级延迟基准，因此本区块不使用示例终端数字作为性能结论。
          </p>
        </Reveal>

        <div className="mt-12 grid gap-4 sm:grid-cols-2">
          {components.map((c, i) => (
            <Reveal key={c.num} className="min-w-0" delay={(i % 2) * 90}>
              <article className="flex h-full flex-col rounded-lg border border-border bg-white p-6 transition hover:border-accent">
                <div className="flex items-center justify-between gap-4 font-mono text-sm">
                  <span className="font-semibold text-accent">{c.num}</span>
                  <span className="text-xs text-muted-foreground">{c.status}</span>
                </div>
                <h3 className="mt-4 text-lg font-semibold">{c.titleEn}</h3>
                <p className="text-sm text-muted-foreground">{c.title}</p>
                <dl className="mt-5 space-y-4 text-sm leading-relaxed">
                  <div>
                    <dt className="font-mono text-xs font-semibold text-foreground">职责</dt>
                    <dd className="mt-1 text-muted-foreground">{c.responsibility}</dd>
                  </div>
                  <div>
                    <dt className="font-mono text-xs font-semibold text-foreground">输入 / 输出</dt>
                    <dd className="mt-1 text-muted-foreground">{c.io}</dd>
                  </div>
                  <div>
                    <dt className="font-mono text-xs font-semibold text-foreground">已知限制</dt>
                    <dd className="mt-1 text-muted-foreground">{c.limitation}</dd>
                  </div>
                </dl>
                <a
                  href={c.evidence}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-5 w-fit text-sm font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
                >
                  {c.evidenceLabel} <span aria-hidden="true">↗</span>
                </a>
              </article>
            </Reveal>
          ))}
        </div>

        <Reveal delay={120}>
          <aside className="mt-10 grid gap-4 border-t border-border pt-6 text-sm md:grid-cols-[minmax(0,1fr)_auto] md:items-start">
            <div>
              <p className="font-mono text-xs font-semibold text-accent">技术内容责任归属</p>
              <p className="mt-2 max-w-2xl leading-relaxed text-muted-foreground">
                本区块由 {siteIdentity.operator.legalName}（{siteIdentity.operator.brandName}）维护，内容依据仓库代码、测试与设计文档更新。最后更新：{siteIdentity.lastUpdated}。
              </p>
            </div>
            <div className="flex flex-wrap gap-x-5 gap-y-2 md:justify-end">
              <Link
                href="/about"
                className="font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
              >
                查看维护主体
              </Link>
              <Link
                href="/contact"
                className="font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
              >
                反馈技术内容
              </Link>
            </div>
          </aside>
        </Reveal>
      </div>
    </section>
  );
}
