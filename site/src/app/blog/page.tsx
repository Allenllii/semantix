import type { Metadata } from "next";
import Link from "next/link";
import { listBlogPosts } from "@/lib/blog";

export const metadata: Metadata = {
  title: "Blog | Semantix",
  description: "Semantix 的语义记忆、跨会话复用与 Agent 基础设施文章。",
  alternates: { canonical: "/blog" },
};

export default function BlogPage() {
  const posts = listBlogPosts();

  return (
    <>
      <header className="border-b border-border bg-muted/30">
        <div className="wrap py-16 md:py-20">
          <p className="font-mono text-xs font-semibold text-accent">SEMANTIX BLOG</p>
          <h1 className="mt-5 max-w-5xl text-4xl font-semibold tracking-tight md:text-6xl">
            语义记忆与 Agent 基础设施。
          </h1>
          <p className="mt-6 max-w-2xl text-lg leading-8 text-muted-foreground">
            关于跨会话复用、检索架构和开源 Agent memory 的研究与实践。
          </p>
          <p className="mt-5 max-w-3xl border-l-2 border-accent pl-4 text-sm leading-6 text-muted-foreground">
            编辑说明：目录中的日期来自仓库版本记录，作者链接指向可核验的 GitHub 贡献历史。文章中的测试结果是仓库内的一手工程证据，不等同于独立生产评测。
          </p>
        </div>
      </header>

      <section className="wrap py-12 md:py-16" aria-labelledby="latest-posts">
        <div className="mb-8 flex items-end justify-between gap-6">
          <h2 id="latest-posts" className="text-2xl font-semibold tracking-tight">
            全部文章
          </h2>
          <span className="font-mono text-xs text-muted-foreground">{posts.length} 篇</span>
        </div>

        <div className="border-t border-border">
          {posts.map((post) => (
            <Link
              key={post.slug}
              href={`/blog/${post.slug}`}
              className="group grid gap-4 border-b border-border py-8 transition-colors hover:bg-muted/35 md:grid-cols-[130px_minmax(0,1fr)_32px] md:px-4"
            >
              <time dateTime={post.updated} className="font-mono text-xs text-muted-foreground">
                {post.updated}
              </time>
              <div>
                <h3 className="max-w-3xl text-xl font-semibold leading-snug tracking-tight transition-colors group-hover:text-accent md:text-2xl">
                  {post.title}
                </h3>
                <p className="mt-3 max-w-3xl text-sm leading-7 text-muted-foreground">
                  {post.description}
                </p>
              </div>
              <span
                aria-hidden="true"
                className="hidden text-xl text-muted-foreground transition-transform group-hover:translate-x-1 group-hover:text-accent md:block"
              >
                →
              </span>
            </Link>
          ))}
        </div>
      </section>
    </>
  );
}
