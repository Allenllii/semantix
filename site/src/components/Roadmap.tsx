import Reveal from "@/components/Reveal";

type RoadmapStep = {
  phase: string;
  titleEn: string;
  title: string;
  body: string;
};

const steps: RoadmapStep[] = [
  {
    phase: "P0",
    titleEn: "Observability",
    title: "可观测层",
    body: "harness 适配器 + 事件流 + 基线指标",
  },
  {
    phase: "P1",
    titleEn: "Slice Library",
    title: "语义切片库",
    body: "提取器 + 嵌入 + ANN 索引，项目/用户双库",
  },
  {
    phase: "P2",
    titleEn: "Semantic Cache",
    title: "语义缓存",
    body: "L2 稳定注入 + L3 验证复用 + 污染检测",
  },
  {
    phase: "P3",
    titleEn: "Scheduler",
    title: "调度器",
    body: "intent 分类 + 并发行为学习 + tier 映射",
  },
  {
    phase: "P4",
    titleEn: "Prefetcher",
    title: "预取器",
    body: "T-Slice 转移矩阵 + 路径模式 + 预算控制",
  },
  {
    phase: "P5",
    titleEn: "Evolution Loop",
    title: "进化闭环",
    body: "在线 EWMA + 离线重训 + ablation",
  },
];

export default function Roadmap() {
  return (
    <section id="roadmap" className="relative overflow-hidden py-24">
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 hidden lg:block">
        <div className="absolute -right-48 bottom-10 h-[380px] w-[380px] rounded-full bg-[oklch(0.524_0.12_165/0.05)] blur-3xl" />
      </div>
      <div className="relative wrap">
        <Reveal>
          <p className="font-mono text-sm font-medium text-accent">
            Roadmap 路线图
          </p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight md:text-4xl">
            从可观测到自进化。
          </h2>
          <p className="mt-1 text-muted-foreground">
            Five phases to a closed loop.
          </p>
          <p className="mt-4 max-w-2xl text-muted-foreground">
            每一步都建立在前一步之上：先能看见，再能复用，然后能调度、能预取，最后整个循环自己进化。
          </p>
        </Reveal>

        <div className="mt-12 space-y-4">
          {steps.map((s, i) => (
            <Reveal key={s.phase} delay={i * 60}>
              <div
                className="grid grid-cols-[auto_1fr] items-start gap-4"
              >
                <span className="w-fit rounded-md border border-accent/30 bg-accent/10 px-2.5 py-1 font-mono text-xs font-semibold text-accent">
                  {s.phase}
                </span>
                <div>
                  <b className="font-semibold">
                    {s.title} {s.titleEn}
                  </b>
                  <p className="mt-0.5 text-sm text-muted-foreground">{s.body}</p>
                </div>
              </div>
            </Reveal>
          ))}
        </div>
      </div>
    </section>
  );
}
