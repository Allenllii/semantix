import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { geoDocuments, getGeoDocument, readGeoDocument } from "@/lib/geo-docs";
import { siteIdentity } from "@/lib/site-identity";
import { getContentAuthor, personJsonLd } from "@/lib/content-authors";

type GeoPageProps = {
  params: Promise<{ slug: string }>;
};

export const dynamicParams = false;

export function generateStaticParams() {
  return geoDocuments.map(({ slug }) => ({ slug }));
}

export async function generateMetadata({ params }: GeoPageProps): Promise<Metadata> {
  const { slug } = await params;
  const document = getGeoDocument(slug);

  if (!document) return {};

  return {
    title: `${document.title} | Semantix`,
    description: document.description,
  };
}

export default async function GeoDocumentPage({ params }: GeoPageProps) {
  const { slug } = await params;
  const document = getGeoDocument(slug);

  if (!document) notFound();

  const content = await readGeoDocument(document);
  const sourceUrl = `${siteIdentity.repositoryUrl}/blob/main/site/content/geo/${document.fileName}`;
  const isEnglish = document.language === "English";
  const author = getContentAuthor(`docs/${document.slug}`);

  const articleJsonLd = {
    "@context": "https://schema.org",
    "@type": "TechArticle",
    "@id": `${siteIdentity.productUrl}/docs/${document.slug}#article`,
    headline: document.title,
    description: document.description,
    datePublished: document.published,
    dateModified: document.lastUpdated,
    inLanguage: isEnglish ? "en" : "zh-CN",
    mainEntityOfPage: `${siteIdentity.productUrl}/docs/${document.slug}`,
    author: personJsonLd(author),
    publisher: { "@id": `${siteIdentity.operator.url}#organization` },
    citation: sourceUrl,
  };

  return (
    <div className="px-6 py-10 md:px-10 md:py-14 lg:px-14">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify(articleJsonLd).replace(/</g, "\\u003c"),
        }}
      />
      <div className="mx-auto max-w-4xl">
        <div className="mb-8 flex flex-wrap items-center justify-between gap-4 border-b border-border pb-5">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Link href="/docs" className="font-medium hover:text-accent">文档</Link>
            <span aria-hidden="true">/</span>
            <span>{document.depth}</span>
          </div>
          <div className="flex flex-wrap items-center gap-3 font-mono text-xs text-muted-foreground">
            <a href={author.url} target="_blank" rel="author noopener noreferrer" className="hover:text-accent">
              {isEnglish ? `By ${author.name}` : `作者 · ${author.name}`}
            </a>
            <span>{document.language}</span>
            <time dateTime={document.published}>Published · {document.published}</time>
            <time dateTime={document.lastUpdated}>Last updated · {document.lastUpdated}</time>
          </div>
        </div>

        <aside className="mb-8 max-w-3xl border-l-2 border-accent bg-muted/40 px-5 py-4 text-sm leading-6 text-muted-foreground">
          <p>
            {isEnglish
              ? "Evidence and limitations: implementation claims should be checked against the current main branch. This document explains the design; it is not an independent production benchmark."
              : "证据与限制：实现状态应以 main 分支的代码与测试为准。本文用于解释设计，不代表独立生产环境基准。"}
          </p>
          <a
            href={sourceUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="mt-2 inline-block font-medium text-foreground underline decoration-border underline-offset-4 hover:text-accent"
          >
            {isEnglish ? "View source and revision history ↗" : "查看原文与修订记录 ↗"}
          </a>
          <p className="mt-3 text-xs">{isEnglish ? "Evidence label: E0 design or editorial context. " : "证据等级：E0，设计或编辑背景。"}{isEnglish ? "See the benchmark boundary before treating an implementation statement as measured evidence." : "如需查看已复现的仓库测试与实验夹具，请先阅读证据页。"} <Link href="/benchmarks" className="underline underline-offset-4 hover:text-accent">{isEnglish ? "Evidence page ↗" : "证据页 ↗"}</Link></p>
        </aside>

        <article className="geo-prose max-w-3xl">
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{
              h1: () => <h1>{document.title}</h1>,
            }}
          >
            {content}
          </ReactMarkdown>
        </article>
      </div>
    </div>
  );
}
