import type { Metadata } from "next";
import BlogHeader from "@/components/blog/BlogHeader";
import BlogSidebar from "@/components/blog/BlogSidebar";
import { listBlogPosts } from "@/lib/blog";

export const metadata: Metadata = {
  title: "Semantix Documentation",
  description: "Guides to semantic slices, cross-session caching, agent scheduling, and framework-neutral memory with Semantix.",
};

export default function BlogLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  const posts = listBlogPosts();
  return (
    <div className="min-h-[100dvh] bg-background text-foreground">
      <BlogHeader />
      <div className="grid min-h-[calc(100dvh-4rem)] lg:grid-cols-[290px_minmax(0,1fr)]">
        <BlogSidebar posts={posts} />
        <main className="min-w-0 border-border lg:border-l">{children}</main>
      </div>
    </div>
  );
}
