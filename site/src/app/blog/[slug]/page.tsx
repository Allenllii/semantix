import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { getBlogPost, listBlogPosts, readBlogPost } from "@/lib/blog";
import { siteIdentity } from "@/lib/site-identity";

type BlogPageProps = { params: Promise<{ slug: string }> };

export const dynamicParams = false;

export function generateStaticParams() {
  return listBlogPosts().map(({ slug }) => ({ slug }));
}

export async function generateMetadata({ params }: BlogPageProps): Promise<Metadata> {
  const post = getBlogPost((await params).slug);
  return post
    ? {
        title: `${post.title} | Semantix`,
        description: post.description,
        alternates: { canonical: `/blog/${post.slug}` },
      }
    : {};
}

export default async function BlogArticlePage({ params }: BlogPageProps) {
  const postMeta = getBlogPost((await params).slug);
  if (!postMeta) notFound();
  const post = readBlogPost(postMeta);
  const sourceUrl = `${siteIdentity.repositoryUrl}/blob/main/blog/${post.fileName}`;
  const articleJsonLd = {
    "@context": "https://schema.org",
    "@type": "BlogPosting",
    "@id": `${siteIdentity.productUrl}/blog/${post.slug}#article`,
    headline: post.title,
    description: post.description,
    dateModified: post.updated,
    datePublished: post.updated,
    inLanguage: "en",
    mainEntityOfPage: `${siteIdentity.productUrl}/blog/${post.slug}`,
    author: { "@id": `${siteIdentity.operator.url}#organization` },
    publisher: { "@id": `${siteIdentity.operator.url}#organization` },
    citation: sourceUrl,
  };

  return (
    <div className="px-6 py-10 md:py-14">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(articleJsonLd).replace(/</g, "\\u003c") }}
      />
      <div className="mx-auto max-w-4xl">
        <div className="mb-8 flex flex-wrap items-center justify-between gap-4 border-b border-border pb-5">
          <Link href="/blog" className="text-sm font-medium text-muted-foreground hover:text-accent">
            ← 返回 Blog
          </Link>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2 font-mono text-xs text-muted-foreground">
            <Link href="/about" className="hover:text-accent">Semantix maintainers</Link>
            <span aria-hidden="true">·</span>
            <time dateTime={post.updated}>Updated {post.updated}</time>
            <span aria-hidden="true">·</span>
            <a href={sourceUrl} target="_blank" rel="noopener noreferrer" className="hover:text-accent">
              Source ↗
            </a>
          </div>
        </div>

        <article className="geo-prose max-w-3xl">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{ h1: () => <h1>{post.title}</h1> }}
          >
            {post.content}
          </ReactMarkdown>
        </article>
      </div>
    </div>
  );
}
