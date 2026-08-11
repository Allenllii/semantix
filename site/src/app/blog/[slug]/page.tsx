import type { Metadata } from "next";
import type { ReactNode } from "react";
import Link from "next/link";
import { notFound } from "next/navigation";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import BlogTableOfContents from "@/components/blog/BlogTableOfContents";
import { getBlogPost, headingId, listBlogPosts, readBlogPost } from "@/lib/blog";
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

function textFromChildren(children: ReactNode): string {
  if (typeof children === "string" || typeof children === "number") return String(children);
  if (Array.isArray(children)) return children.map(textFromChildren).join("");
  if (children && typeof children === "object" && "props" in children) {
    return textFromChildren((children as { props: { children?: ReactNode } }).props.children);
  }
  return "";
}

export default async function BlogArticlePage({ params }: BlogPageProps) {
  const postMeta = getBlogPost((await params).slug);
  if (!postMeta) notFound();
  const post = readBlogPost(postMeta);
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
  };

  return (
    <div className="px-6 py-10 md:px-10 lg:px-14 lg:py-14">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(articleJsonLd).replace(/</g, "\\u003c") }}
      />
      <div className="mx-auto grid max-w-[1120px] gap-14 xl:grid-cols-[minmax(0,780px)_220px]">
        <div className="min-w-0">
          <div className="mb-9 border-b border-border pb-7">
            <Link href="/blog" className="text-sm font-medium text-muted-foreground transition-colors hover:text-accent">Documentation</Link>
            <p className="mt-5 font-mono text-xs text-accent">{post.group}</p>
            <h1 className="mt-3 text-4xl font-semibold leading-tight tracking-tight md:text-5xl">{post.title}</h1>
            <p className="mt-5 text-lg leading-8 text-muted-foreground">{post.description}</p>
            <time dateTime={post.updated} className="mt-5 block font-mono text-xs text-muted-foreground">
              Updated {post.updated}<span className="sr-only">Last updated</span>
            </time>
          </div>

          <article className="blog-prose">
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              components={{
                h1: () => null,
                h2: ({ children }) => <h2 id={headingId(textFromChildren(children))}>{children}</h2>,
                h3: ({ children }) => <h3 id={headingId(textFromChildren(children))}>{children}</h3>,
              }}
            >
              {post.content}
            </ReactMarkdown>
          </article>
        </div>
        <BlogTableOfContents headings={post.headings} />
      </div>
    </div>
  );
}
