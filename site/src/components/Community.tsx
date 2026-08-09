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
      <div className="relative wrap text-center">
        <Reveal>
          <p className="font-mono text-sm font-medium text-accent">
            Community 社区
          </p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight md:text-4xl">
            开源共建。
          </h2>
          <p className="mt-1 text-muted-foreground">Built in the open.</p>
          <p className="mx-auto mt-4 max-w-xl text-muted-foreground">
            Semantix 采用 MIT 许可，公开开发。每一个 issue、每一条 PR、每一次深夜
            review，都在让这个内核变得更好。
          </p>
        </Reveal>

        <Reveal delay={100}>
          <div className="relative mt-10 max-w-2xl mx-auto overflow-hidden py-2 [mask-image:linear-gradient(to_right,transparent,black_10%,black_90%,transparent)]">
            <div className="crew-row">
              {[...contributors, ...contributors].map((login, i) => (
                <a
                  key={`a-${i}`}
                  href={`https://github.com/${login}`}
                  target="_blank"
                  rel="noopener"
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
                  rel="noopener"
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
        </Reveal>

        <Reveal delay={180}>
          <div className="mt-10 flex justify-center gap-3">
          <a
            href="https://github.com/Gnosil/semantix"
            target="_blank"
            rel="noopener"
            className="rounded-md bg-accent px-5 py-2.5 font-medium text-white hover:opacity-90"
          >
            在 GitHub 上参与 ↗
          </a>
          <a
            href="https://github.com/Gnosil/semantix/issues"
            target="_blank"
            rel="noopener"
            className="rounded-md border border-border px-5 py-2.5 text-sm hover:border-accent"
          >
            Good first issues →
          </a>
          </div>
        </Reveal>
      </div>
    </section>
  );
}
