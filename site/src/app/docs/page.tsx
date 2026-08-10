import type { Metadata } from "next";
import Link from "next/link";
import { geoDocuments } from "@/lib/geo-docs";

export const metadata: Metadata = {
  title: "文档 | Semantix",
  description: "在 Semantix 官网阅读项目说明与深度指南。",
};

export default function GeoIndexPage() {
  return (
    <main className="min-h-screen bg-background pt-16">
      <section className="border-b border-border bg-muted/40 py-20 md:py-28">
        <div className="wrap">
          <p className="mb-4 font-mono text-sm font-semibold uppercase tracking-[0.18em] text-accent">
            Documentation · Project knowledge
          </p>
          <h1 className="max-w-3xl text-4xl font-semibold tracking-tight text-foreground md:text-6xl">
            Semantix 文档
          </h1>
          <p className="mt-6 max-w-2xl text-lg leading-8 text-muted-foreground">
            由官网同源提供的项目说明，帮助开发者和 AI 引擎快速理解 Semantix
            的定位、架构、边界与路线图。
          </p>
        </div>
      </section>

      <section className="wrap py-14 md:py-20">
        <div className="grid gap-4 md:grid-cols-2">
          {geoDocuments.map((document) => (
            <Link
              key={document.slug}
              href={`/docs/${document.slug}`}
              className="group rounded-2xl border border-border bg-card p-6 transition-colors hover:border-accent md:p-8"
            >
              <div className="mb-8 flex items-center justify-between gap-4">
                <span className="rounded-full bg-muted px-3 py-1 font-mono text-xs text-muted-foreground">
                  {document.language}
                </span>
                <span className="font-mono text-xs text-accent">{document.depth}</span>
              </div>
              <h2 className="text-2xl font-semibold tracking-tight text-foreground">
                {document.title}
              </h2>
              <p className="mt-3 leading-7 text-muted-foreground">{document.description}</p>
              <span className="mt-8 inline-flex text-sm font-semibold text-foreground transition-colors group-hover:text-accent">
                阅读文档 <span aria-hidden="true">→</span>
              </span>
            </Link>
          ))}
        </div>
      </section>
    </main>
  );
}
