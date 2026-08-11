import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { geoDocuments, getGeoDocument, readGeoDocument } from "@/lib/geo-docs";
import { siteIdentity } from "@/lib/site-identity";

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

  const articleJsonLd = {
    "@context": "https://schema.org",
    "@type": "TechnicalArticle",
    "@id": `${siteIdentity.productUrl}/docs/${document.slug}#article`,
    headline: document.title,
    description: document.description,
    dateModified: document.lastUpdated,
    inLanguage: document.language === "English" ? "en" : "zh-CN",
    mainEntityOfPage: `${siteIdentity.productUrl}/docs/${document.slug}`,
    author: { "@id": `${siteIdentity.operator.url}#organization` },
    publisher: { "@id": `${siteIdentity.operator.url}#organization` },
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
            <span>{document.language}</span>
            <time dateTime={document.lastUpdated}>Last updated · {document.lastUpdated}</time>
          </div>
        </div>

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
