import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "文档 | Semantix",
  description: "在 Semantix 官网阅读项目说明与深度指南。",
};

export default function GeoIndexPage() {
  return (
    <div className="px-6 py-10 md:px-10 md:py-14 lg:px-14">
      <div className="mx-auto max-w-4xl">
        <p className="font-mono text-xs font-semibold text-accent">SEMANTIX 文档</p>
        <h1 className="mt-5 text-4xl font-semibold tracking-tight text-foreground md:text-5xl">
          从项目定位到核心机制。
        </h1>
        <p className="mt-5 max-w-2xl text-lg leading-8 text-muted-foreground">
          文档已集中到左侧目录。建议先阅读项目速览，再根据语言和深度进入对应文档。
        </p>

        <section className="mt-12 max-w-2xl border-t border-border pt-8">
          <h2 className="text-xl font-semibold">阅读方式</h2>
          <div className="mt-5 space-y-5 text-sm leading-7 text-muted-foreground">
            <p>
              桌面端从左侧目录选择文档，目录会保持在视口内并标记当前页面。正文在右侧独立阅读。
            </p>
            <p>
              手机端点击正文上方的“文档目录”展开全部入口。中文与英文版本按速览和深入两组排列。
            </p>
          </div>
        </section>
      </div>
    </div>
  );
}
