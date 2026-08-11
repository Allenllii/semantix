"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { cn } from "@/lib/utils";

type DocsNavDocument = {
  slug: string;
  title: string;
  language: string;
  depth: "速览" | "深入";
};

function DocumentLinks({
  pathname,
  documents,
}: {
  pathname: string;
  documents: readonly DocsNavDocument[];
}) {
  const groups = [
    {
      title: "项目速览",
      documents: documents.filter((document) => document.depth === "速览"),
    },
    {
      title: "架构深读",
      documents: documents.filter((document) => document.depth === "深入"),
    },
  ];

  return (
    <nav aria-label="文档目录" className="space-y-7">
      <div>
        <Link
          href="/docs"
          className={cn(
            "block rounded-md px-3 py-2.5 text-sm transition-colors",
            pathname === "/docs"
              ? "bg-accent/10 font-semibold text-accent"
              : "text-muted-foreground hover:bg-muted hover:text-foreground",
          )}
        >
          文档首页
          <span className="mt-0.5 block text-xs font-normal opacity-75">全部文档与阅读说明</span>
        </Link>
      </div>

      {groups.map((group) => (
        <div key={group.title}>
          <p className="mb-2 px-3 text-xs font-semibold text-foreground">{group.title}</p>
          <div className="space-y-1">
            {group.documents.map((document) => {
              const href = `/docs/${document.slug}`;
              const active = pathname === href;

              return (
                <Link
                  key={document.slug}
                  href={href}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "block rounded-md px-3 py-2.5 text-sm transition-colors",
                    active
                      ? "bg-accent/10 font-semibold text-accent"
                      : "text-muted-foreground hover:bg-muted hover:text-foreground",
                  )}
                >
                  {document.title}
                  <span className="mt-0.5 block font-mono text-[11px] font-normal opacity-70">
                    {document.language}
                  </span>
                </Link>
              );
            })}
          </div>
        </div>
      ))}

      <div>
        <p className="mb-2 px-3 text-xs font-semibold text-foreground">帮助与参考</p>
        <Link
          href="/docs/faq"
          aria-current={pathname === "/docs/faq" ? "page" : undefined}
          className={cn(
            "block rounded-md px-3 py-2.5 text-sm transition-colors",
            pathname === "/docs/faq"
              ? "bg-accent/10 font-semibold text-accent"
              : "text-muted-foreground hover:bg-muted hover:text-foreground",
          )}
        >
          常见问题
          <span className="mt-0.5 block text-xs font-normal opacity-75">定位、缓存与参与方式</span>
        </Link>
      </div>

      <div className="border-t border-border pt-5">
        <a
          href="https://github.com/Gnosil/semantix"
          target="_blank"
          rel="noopener noreferrer"
          className="block px-3 text-sm font-medium text-muted-foreground transition-colors hover:text-accent"
        >
          GitHub 仓库 <span aria-hidden="true">↗</span>
        </a>
      </div>
    </nav>
  );
}

export default function DocsSidebar({ documents }: { documents: readonly DocsNavDocument[] }) {
  const pathname = usePathname();

  return (
    <>
      <div className="border-b border-border bg-white px-5 py-4 lg:hidden">
        <details className="group">
          <summary className="flex cursor-pointer list-none items-center justify-between font-semibold">
            文档目录
            <span aria-hidden="true" className="text-muted-foreground transition-transform group-open:rotate-180">
              ↓
            </span>
          </summary>
          <div className="pt-5">
            <DocumentLinks pathname={pathname} documents={documents} />
          </div>
        </details>
      </div>

      <aside className="hidden bg-[oklch(0.992_0.002_165)] lg:sticky lg:top-16 lg:block lg:h-[calc(100dvh-4rem)] lg:overflow-y-auto">
        <div className="px-5 py-8">
          <p className="mb-7 px-3 font-mono text-xs font-semibold text-accent">SEMANTIX 文档</p>
          <DocumentLinks pathname={pathname} documents={documents} />
        </div>
      </aside>
    </>
  );
}
