import Reveal from "@/components/Reveal";

type ComponentCard = {
  icon: string;
  titleEn: string;
  title: string;
  body: string;
};

const components: ComponentCard[] = [
  {
    icon: "🗂️",
    titleEn: "Semantic Slice Library",
    title: "语义切片库",
    body: "五种切片类型 P/C/T/R/M，turn 边界切分 + 完成点分段 + T-Slice n-gram 提取，bbolt 双库持久化。",
  },
  {
    icon: "⚡",
    titleEn: "Semantic Cache L1-L3",
    title: "语义缓存",
    body: "L1 字节缓存、L2 稳定前缀注入、L3 验证后复用——三级逐层兜底，fail-open 不阻塞主循环。",
  },
  {
    icon: "🧭",
    titleEn: "Kernel Scheduler",
    title: "内核调度器",
    body: "intent 识别 + 并发行为学习 + 模型 tier 映射，按任务类型做联合决策。",
  },
  {
    icon: "⏩",
    titleEn: "Speculative Prefetch",
    title: "投机预取",
    body: "T-Slice 转移矩阵预测下一步，流式等待期预取只读资源，waste 自惩罚。",
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
            Components 内核组件
          </p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight md:text-4xl">
            一个内核，四个组件。
          </h2>
          <p className="mt-1 text-muted-foreground">One kernel, four components.</p>
          <p className="mt-4 max-w-2xl text-muted-foreground">
            切片库负责沉淀，缓存负责变现，调度器负责编排，预取器负责填空闲——四个组件共享同一套事件与参数存储，单向依赖，互不循环。
          </p>
        </Reveal>

        <div className="mt-12 grid gap-4 sm:grid-cols-2">
          {components.map((c, i) => (
            <Reveal key={c.titleEn} delay={(i % 2) * 90}>
              <article
                className="rounded-lg border border-border bg-white p-6 transition hover:border-accent"
              >
                <div className="flex h-8 w-8 items-center justify-center rounded-md border border-accent/30 bg-accent/10 text-lg">
                  {c.icon}
                </div>
                <h3 className="mt-4 text-lg font-semibold">{c.titleEn}</h3>
                <p className="text-sm text-muted-foreground">{c.title}</p>
                <p className="mt-2 text-sm text-muted-foreground">{c.body}</p>
              </article>
            </Reveal>
          ))}
        </div>
      </div>
    </section>
  );
}
