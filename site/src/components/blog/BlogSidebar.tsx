"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import BlogSearch from "@/components/blog/BlogSearch";
import { BLOG_GROUPS, type BlogPostMeta } from "@/lib/blog-shared";
import { cn } from "@/lib/utils";

function SidebarContent({ posts }: { posts: BlogPostMeta[] }) {
  const pathname = usePathname();
  const normalizedPathname = pathname === "/" ? pathname : pathname.replace(/\/$/, "");
  const [query, setQuery] = useState("");
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return needle ? posts.filter((post) => post.title.toLowerCase().includes(needle)) : posts;
  }, [posts, query]);

  return (
    <div className="space-y-7">
      <BlogSearch value={query} onChange={setQuery} />
      <nav aria-label="Blog article directory" className="space-y-7">
        <Link
          href="/blog"
          className={cn(
            "block rounded-md px-3 py-2 text-sm font-medium transition-colors",
            normalizedPathname === "/blog" ? "bg-accent/10 text-accent" : "text-muted-foreground hover:bg-muted hover:text-foreground",
          )}
        >
          Documentation home
        </Link>
        {BLOG_GROUPS.map((group) => {
          const groupPosts = filtered.filter((post) => post.group === group);
          if (!groupPosts.length) return null;
          return (
            <section key={group}>
              <h2 className="mb-2 px-3 font-mono text-[11px] font-semibold uppercase tracking-[0.08em] text-foreground">
                {group}
              </h2>
              <div className="space-y-0.5">
                {groupPosts.map((post) => {
                  const href = `/blog/${post.slug}`;
                  const active = normalizedPathname === href;
                  return (
                    <Link
                      key={post.slug}
                      href={href}
                      aria-current={active ? "page" : undefined}
                      className={cn(
                        "block rounded-md px-3 py-2 text-[13px] leading-5 transition-colors",
                        active
                          ? "bg-accent/10 font-semibold text-accent"
                          : "text-muted-foreground hover:bg-muted hover:text-foreground",
                      )}
                    >
                      {post.title}
                    </Link>
                  );
                })}
              </div>
            </section>
          );
        })}
        {!filtered.length && <p className="px-3 text-sm text-muted-foreground">No matching articles.</p>}
      </nav>
    </div>
  );
}

export default function BlogSidebar({ posts }: { posts: BlogPostMeta[] }) {
  return (
    <>
      <div className="border-b border-border bg-white px-5 py-4 lg:hidden">
        <details>
          <summary className="cursor-pointer font-medium">Browse documentation</summary>
          <div className="pt-5"><SidebarContent posts={posts} /></div>
        </details>
      </div>
      <aside className="hidden bg-[oklch(0.992_0.002_165)] lg:sticky lg:top-16 lg:block lg:h-[calc(100dvh-4rem)] lg:overflow-y-auto">
        <div className="px-4 py-6"><SidebarContent posts={posts} /></div>
      </aside>
    </>
  );
}
