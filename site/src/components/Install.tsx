import Link from "next/link";
import Reveal from "@/components/Reveal";
import CopyCode from "@/components/CopyCode";

const steps = [
  {
    number: "01",
    title: "克隆仓库",
    titleEn: "Clone",
    desc: "把 semantix 拉到本地，开始贡献或自用。",
    code: "git clone https://github.com/Gnosil/semantix.git",
    href: "https://github.com/Gnosil/semantix",
    external: true,
    linkLabel: "仓库 →",
  },
  {
    number: "02",
    title: "构建 CLI",
    titleEn: "Build",
    desc: "一条命令编译 extract / search 工具。",
    code: "go build ./cmd/semantix",
    href: "https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md",
    external: true,
    linkLabel: "构建步骤 ↗",
  },
  {
    number: "03",
    title: "提取并检索",
    titleEn: "Extract & Search",
    desc: "从会话中提取切片，用 BM25 检索。",
    code: "semantix extract -session s.jsonl -scope project",
    href: "/docs/guide",
    external: false,
    linkLabel: "深度文档 →",
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
            从源码跑起来。
          </h2>
          <p className="mt-1 text-muted-foreground">
            Clone, build, extract — then verify the results yourself.
          </p>
        </Reveal>

        <div className="mt-12 grid gap-4 md:grid-cols-3">
          {steps.map((step, i) => (
            <Reveal key={step.number} delay={i * 80}>
              <article className="flex h-full flex-col rounded-lg border border-border bg-white p-6 transition hover:border-accent">
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
                <CopyCode className="mt-4" code={step.code} tone="dark" />
                <div className="mt-auto pt-4 text-sm">
                  {step.external ? (
                    <a
                      href={step.href}
                      target="_blank"
                      rel="noopener"
                      className="text-accent hover:underline"
                    >
                      {step.linkLabel}
                    </a>
                  ) : (
                    <Link href={step.href} className="text-accent hover:underline">
                      {step.linkLabel}
                    </Link>
                  )}
                </div>
              </article>
            </Reveal>
          ))}
        </div>

        <Reveal delay={240}>
          <div className="mt-10 flex flex-wrap items-center justify-center gap-3">
            <a
              href="https://github.com/Gnosil/semantix/blob/main/docs/QUICKSTART.md"
              target="_blank"
              rel="noopener"
              className="rounded-md bg-accent px-5 py-2.5 font-medium text-white hover:opacity-90"
            >
              运行离线验证（verify）↗
            </a>
            <Link
              href="/docs/guide"
              className="rounded-md border border-border px-5 py-2.5 text-sm hover:border-accent"
            >
              阅读架构文档 →
            </Link>
            <a
              href="https://github.com/Gnosil/semantix/blob/main/CONTRIBUTING.md"
              target="_blank"
              rel="noopener"
              className="rounded-md border border-border px-5 py-2.5 text-sm hover:border-accent"
            >
              参与贡献 →
            </a>
          </div>
        </Reveal>

        <Reveal delay={300}>
          <p className="mt-10 text-center text-sm text-muted-foreground">
            维护者：
            <a
              href="https://github.com/Gnosil"
              target="_blank"
              rel="noopener"
              className="text-accent hover:underline"
            >
              Gnosil
            </a>
            {"、"}
            <a
              href="https://github.com/radianceded"
              target="_blank"
              rel="noopener"
              className="text-accent hover:underline"
            >
              radianceded
            </a>
            {"、"}
            <a
              href="https://github.com/Allenli1233"
              target="_blank"
              rel="noopener"
              className="text-accent hover:underline"
            >
              Allenli1233
            </a>
            {"、"}
            <a
              href="https://github.com/jh10724-dotcom"
              target="_blank"
              rel="noopener"
              className="text-accent hover:underline"
            >
              jh10724-dotcom
            </a>
            {" · "}© 2026 MIT License · 技术作者：Gnosil
          </p>
        </Reveal>
      </div>
    </section>
  );
}
