import Reveal from "@/components/Reveal";

const contributors = [
  "Gnosil",
  "radianceded",
  "jh10724-dotcom",
  "Allenli1233",
  "lr",
];

const avatarColors = [
  "bg-[oklch(0.608_0.14_165)]",
  "bg-[oklch(0.524_0.12_165)]",
  "bg-[oklch(0.45_0.1_165)]",
  "bg-[oklch(0.35_0.08_165)]",
];

export default function Community() {
  return (
    <section id="community" className="relative overflow-hidden py-24 bg-[oklch(0.976_0.005_165)]">
      <div aria-hidden="true" className="pointer-events-none absolute inset-0 hidden lg:block">
        <div className="absolute -right-56 -top-24 h-[440px] w-[440px] rounded-full bg-[oklch(0.608_0.14_165/0.07)] blur-3xl" />
      </div>
      <div className="relative wrap">
        <Reveal>
          <p className="font-mono text-sm font-medium text-accent">
            Community 社区
          </p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight md:text-4xl">
            讨论、变更与贡献都可追溯。
          </h2>
          <p className="mt-4 max-w-2xl text-muted-foreground">
            Semantix 采用 FSL-1.1-MIT 许可。需求讨论记录在 issue，代码变更通过 PR 审阅，贡献者名单以 GitHub 提交记录为准。
          </p>
        </Reveal>

        <Reveal delay={100}>
          <div className="mt-10 grid border-t border-border md:grid-cols-2">
            <div className="border-b border-border py-6 md:border-r md:pr-8">
              <p className="font-mono text-xs font-semibold text-accent">ISSUES</p>
              <h3 className="mt-3 text-lg font-semibold">需求与技术讨论</h3>
              <p className="mt-2 max-w-md text-sm leading-relaxed text-muted-foreground">
                功能范围、验收标准和未决问题在公开 issue 中记录，关闭状态不替代代码与测试验证。
              </p>
              <a
                href="https://github.com/Gnosil/semantix/issues"
                target="_blank"
                rel="noopener noreferrer"
                className="mt-4 inline-block text-sm font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
              >
                查看 Issues <span aria-hidden="true">↗</span>
              </a>
            </div>
            <div className="border-b border-border py-6 md:pl-8">
              <p className="font-mono text-xs font-semibold text-accent">CONTRIBUTORS</p>
              <h3 className="mt-3 text-lg font-semibold">提交与贡献记录</h3>
              <p className="mt-2 max-w-md text-sm leading-relaxed text-muted-foreground">
                沿用原有贡献者展示；详细归属仍以 Git commit、PR 审阅和 GitHub Contributors 页面为准。
              </p>
              <div className="relative mt-5 overflow-hidden py-2 [mask-image:linear-gradient(to_right,transparent,black_10%,black_90%,transparent)]">
                <div className="crew-row">
                  {[...contributors, ...contributors].map((login, i) => (
                    <a
                      key={`a-${i}`}
                      href={`https://github.com/${login}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      title={login}
                    >
                      <span
                        className={`flex h-10 w-10 items-center justify-center rounded-full text-sm font-semibold text-white ${avatarColors[i % avatarColors.length]}`}
                      >
                        {login.charAt(0).toUpperCase()}
                      </span>
                    </a>
                  ))}
                </div>
                <div className="crew-row rev mt-3">
                  {[...contributors, ...contributors].map((login, i) => (
                    <a
                      key={`b-${i}`}
                      href={`https://github.com/${login}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      title={login}
                    >
                      <span
                        className={`flex h-10 w-10 items-center justify-center rounded-full text-sm font-semibold text-white ${avatarColors[(i + 2) % avatarColors.length]}`}
                      >
                        {login.charAt(0).toUpperCase()}
                      </span>
                    </a>
                  ))}
                </div>
              </div>
              <a
                href="https://github.com/Gnosil/semantix/graphs/contributors"
                target="_blank"
                rel="noopener noreferrer"
                className="mt-4 inline-block text-sm font-medium text-foreground underline decoration-border underline-offset-4 transition-colors hover:text-accent hover:decoration-accent"
              >
                查看 Contributors <span aria-hidden="true">↗</span>
              </a>
            </div>
          </div>
        </Reveal>

        <Reveal delay={180}>
          <div className="mt-8 flex flex-wrap gap-3">
          <a
            href="https://github.com/Gnosil/semantix"
            target="_blank"
            rel="noopener noreferrer"
            className="rounded-md bg-accent px-5 py-2.5 font-medium text-white hover:opacity-90"
          >
            查看代码仓库 ↗
          </a>
          </div>
        </Reveal>
      </div>
    </section>
  );
}
