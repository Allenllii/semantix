import type { Metadata } from "next";
import Link from "next/link";
import { BLOG_GROUPS, listBlogPosts } from "@/lib/blog";

export const metadata: Metadata = {
  title: "Semantix Documentation | Semantix",
  description: "Practical guides to reusable semantic slices, cross-session caching, agent tool execution, and framework-neutral memory infrastructure.",
  alternates: { canonical: "/blog" },
};

export default function BlogIndexPage() {
  const posts = listBlogPosts();
  return (
    <div className="px-6 py-12 md:px-10 lg:px-14 lg:py-16">
      <div className="mx-auto max-w-5xl">
        <header className="max-w-3xl border-b border-border pb-10">
          <p className="mb-4 font-mono text-xs font-semibold text-accent">SEMANTIX DOCUMENTATION</p>
          <h1 className="text-4xl font-semibold tracking-tight md:text-5xl">Semantix Documentation</h1>
          <p className="mt-5 max-w-2xl text-lg leading-8 text-muted-foreground">
            Practical guides to reusable semantic slices, cross-session caching, agent tool execution, and framework-neutral memory infrastructure.
          </p>
          <Link href={`/blog/${posts[0].slug}`} className="mt-7 inline-flex rounded-md bg-accent px-4 py-2.5 text-sm font-semibold text-white transition-opacity hover:opacity-90">
            Start reading
          </Link>
        </header>

        <div className="mt-12 space-y-14">
          {BLOG_GROUPS.map((group) => {
            const groupPosts = posts.filter((post) => post.group === group);
            return (
              <section key={group}>
                <h2 className="mb-5 text-xl font-semibold tracking-tight">{group}</h2>
                <div className="grid gap-x-10 gap-y-6 md:grid-cols-2">
                  {groupPosts.map((post) => (
                    <Link key={post.slug} href={`/blog/${post.slug}`} className="group block border-t border-border pt-4">
                      <h3 className="font-semibold leading-6 transition-colors group-hover:text-accent">{post.title}</h3>
                      <p className="mt-2 line-clamp-2 text-sm leading-6 text-muted-foreground">{post.description}</p>
                    </Link>
                  ))}
                </div>
              </section>
            );
          })}
        </div>
      </div>
    </div>
  );
}
