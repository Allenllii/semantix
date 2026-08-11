import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { geoDocuments, getGeoDocument, readGeoDocument } from "@/lib/geo-docs";

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

  return (
    <main className="min-h-screen bg-background pt-16">
      <div className="wrap py-10 md:py-16">
        <div className="mb-10 flex flex-wrap items-center justify-between gap-4 border-b border-border pb-6">
          <Link href="/docs" className="text-sm font-semibold text-muted-foreground hover:text-accent">
            ← 返回文档
          </Link>
          <div className="flex flex-wrap items-center gap-2 font-mono text-xs text-muted-foreground">
            <span className="rounded-full bg-muted px-3 py-1">{document.language}</span>
            <span className="rounded-full bg-muted px-3 py-1">{document.depth}</span>
            <time dateTime={document.lastUpdated} className="rounded-full bg-muted px-3 py-1">
              Last updated · {document.lastUpdated}
            </time>
          </div>
        </div>

        <article className="geo-prose mx-auto max-w-3xl">
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
    </main>
  );
}
