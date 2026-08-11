import Reveal from "@/components/Reveal";
import ParticleCanvas from "@/components/ParticleCanvas";
import CopyCode from "@/components/CopyCode";
import { siteIdentity } from "@/lib/site-identity";

export default function Hero() {
  return (
    <section className="relative overflow-hidden pt-32 pb-20">
      {/* 两侧装饰：光晕 + 淡色 mono 文字 */}
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 hidden lg:block">
        {/* 左光晕 */}
        <div className="absolute -left-40 top-24 h-[420px] w-[420px] rounded-full bg-[oklch(0.608_0.14_165/0.08)] blur-3xl" />
        {/* 右光晕 */}
        <div className="absolute -right-40 bottom-16 h-[380px] w-[380px] rounded-full bg-[oklch(0.524_0.12_165/0.08)] blur-3xl" />
        {/* 左装饰文字 */}
        <div className="absolute left-10 top-1/2 -translate-y-1/2 -rotate-90 origin-left whitespace-nowrap font-mono text-xs tracking-[0.35em] text-[oklch(0.45_0.05_165/0.4)]">
          extract · index · retrieve · evolve
        </div>
        {/* 右装饰文字 */}
        <div className="absolute right-10 top-1/2 -translate-y-1/2 rotate-90 origin-right whitespace-nowrap font-mono text-xs tracking-[0.35em] text-[oklch(0.45_0.05_165/0.4)]">
          L1 · L2 · L3 · semantic cache
        </div>
      </div>
      <div className="relative wrap text-center">
        {/* eyebrow */}
        <Reveal>
          <span className="inline-block rounded-full border border-accent/30 bg-[oklch(0.943_0.05_165)] px-4 py-1.5 font-mono text-xs text-accent">
            Open source · FSL-1.1-MIT · Go
          </span>
        </Reveal>

        {/* headline */}
        <Reveal delay={80}>
          <h1 className="mt-6 text-5xl font-bold tracking-tight md:text-6xl lg:text-7xl">
            A <span className="text-accent">self-evolving</span> agent kernel.
          </h1>
          <p className="mt-4 text-2xl text-muted-foreground md:text-3xl">
            一个自进化的 Agent Kernel。
          </p>
          <time
            dateTime={siteIdentity.lastUpdated}
            className="mt-3 block font-mono text-xs text-muted-foreground"
          >
            Last updated · {siteIdentity.lastUpdated}
          </time>
        </Reveal>

        {/* subheading */}
        <Reveal delay={160}>
          <p className="mx-auto mt-6 max-w-2xl text-muted-foreground">
            Semantix 连接 Agent Harness 与资源层，探索跨会话语义复用。
            <br className="hidden md:block" />
            当前版本已实现会话切片、BM25 检索与跨会话注入；调度、预取和自进化闭环仍在开发中。
          </p>
        </Reveal>

        {/* install cards */}
        <Reveal delay={240}>
          <div className="mx-auto mt-10 grid max-w-2xl grid-cols-1 gap-4 text-left sm:grid-cols-2">
          <div className="relative overflow-hidden rounded-lg border border-border bg-white p-5 transition-colors hover:border-accent">
            <ParticleCanvas className="pointer-events-none absolute inset-0 opacity-45" />
            <div className="relative">
              <h3 className="font-semibold">GitHub 仓库</h3>
              <p className="mt-1 text-sm text-muted-foreground">
                代码、测试、设计文档与路线图公开可查
              </p>
              <a
                href="https://github.com/Gnosil/semantix"
                target="_blank"
                rel="noopener noreferrer"
                className="mt-3 inline-block rounded-md border border-border px-3 py-1.5 text-sm transition-colors hover:border-accent"
              >
                GitHub ↗
              </a>
            </div>
          </div>
          <div className="relative overflow-hidden rounded-lg border border-border bg-white p-5 transition-colors hover:border-accent">
            <ParticleCanvas className="pointer-events-none absolute inset-0 opacity-45" />
            <div className="relative">
              <h3 className="font-semibold">CLI</h3>
              <p className="mt-1 text-sm text-muted-foreground">
                extract / search 切片提取与 BM25 检索
              </p>
              <CopyCode
                className="mt-3"
                code="go install semantix/cmd/semantix"
                prompt
              />
            </div>
          </div>
          </div>
        </Reveal>

        {/* terminal demo */}
        <Reveal delay={320}>
          <div className="mx-auto mt-16 max-w-3xl text-left">
          <div className="pane-in overflow-hidden rounded-xl border border-border bg-[oklch(0.21_0.006_260)] shadow-lg">
            <div className="flex items-center gap-2 bg-[oklch(0.27_0.008_260)] px-4 py-3">
              <span className="h-3 w-3 rounded-full bg-[#F87171]" />
              <span className="h-3 w-3 rounded-full bg-[#FBBF24]" />
              <span className="h-3 w-3 rounded-full bg-[#4ADE80]" />
              <span className="ml-2 font-mono text-xs text-slate-400">
                ~/semantix - example output (not a benchmark)
              </span>
            </div>
            <div className="px-5 py-4 font-mono text-sm leading-relaxed text-slate-300">
              <div>
                <span className="text-accent">$ semantix extract</span>{" "}
                <span className="text-slate-400">
                  -session sessions/2026-08-07.jsonl -scope project
                </span>
              </div>
              <div className="text-[#4ADE80]">
                ✓ extracted=512 slices stored=512 scope=project
              </div>
              <div className="text-slate-400">
                {"  "}P-slices 214 · C-slices 158 · T-slices 140
              </div>
              <div>&nbsp;</div>
              <div>
                <span className="text-accent">$ semantix search</span>{" "}
                <span className="text-slate-400">
                  &quot;如何缓存跨会话前缀&quot; -topk 3
                </span>
              </div>
              <div>
                #1 [P] id=9f2a score=7.42 &quot;stable slice injection → vendor byte
                cache&quot;
              </div>
              <div>
                #2 [T] id=3c81 score=6.98 &quot;grep → readFile → editFile → test&quot;
              </div>
              <div>
                #3 [C] id=b704 score=6.51 &quot;L2 freeze-period ≥1h guard&quot;
              </div>
              <div>&nbsp;</div>
              <div className="text-slate-500">
                示例数据 · 实际结果取决于会话内容、数据规模与运行环境
              </div>
              <div>
                <span className="blink inline-block h-4 w-2 bg-emerald-400 align-middle">
                  ▍
                </span>
              </div>
            </div>
          </div>
          </div>
        </Reveal>
      </div>
    </section>
  );
}
