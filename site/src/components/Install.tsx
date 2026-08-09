import Reveal from "@/components/Reveal";

const steps = [
  {
    number: "01",
    title: "克隆仓库",
    titleEn: "Clone",
    desc: "把 semantix 拉到本地，开始贡献或自用。",
    code: "git clone https://github.com/Gnosil/semantix.git",
  },
  {
    number: "02",
    title: "构建 CLI",
    titleEn: "Build",
    desc: "一条命令编译 extract / search 工具。",
    code: "go build ./cmd/semantix",
  },
  {
    number: "03",
    title: "提取并检索",
    titleEn: "Extract & Search",
    desc: "从会话中提取切片，用 BM25 检索。",
    code: "semantix extract -session s.jsonl -scope project",
  },
];

export default function Install() {
  return (
    <section id="start" className="relative overflow-hidden py-24">
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 hidden lg:block">
        <div className="absolute -left-48 top-16 h-[360px] w-[360px] rounded-full bg-[oklch(0.608_0.14_165/0.06)] blur-3xl" />
      </div>
      <div className="relative wrap">
        <Reveal>
          <p className="font-mono text-sm font-medium text-accent">Install 安装</p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight md:text-4xl">
            三分钟跑起来。
          </h2>
          <p className="mt-1 text-muted-foreground">Get started.</p>
        </Reveal>

        <div className="mt-12 grid gap-4 md:grid-cols-3">
          {steps.map((step, i) => (
            <Reveal key={step.number} delay={i * 80}>
              <div
                className="rounded-lg border border-border bg-white p-6 transition hover:border-accent"
              >
                <p className="font-mono text-sm font-semibold text-accent">
                  {step.number}
                </p>
                <h3 className="mt-2 text-lg font-semibold">
                  {step.title}{" "}
                  <span className="font-normal text-muted-foreground">
                    {step.titleEn}
                  </span>
                </h3>
                <p className="mt-1 text-sm text-muted-foreground">{step.desc}</p>
                <pre className="mt-4 overflow-x-auto rounded-md bg-[oklch(0.21_0.006_260)] p-3 font-mono text-xs text-slate-300">
                  {step.code}
                </pre>
              </div>
            </Reveal>
          ))}
        </div>
      </div>
    </section>
  );
}
