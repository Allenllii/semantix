"use client";

import { useEffect } from "react";
import Link from "next/link";

export default function BlogHeader() {
  const focusSearch = () => document.getElementById("blog-search")?.focus();

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        focusSearch();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  return (
    <header className="sticky top-0 z-50 border-b border-border bg-white/95 backdrop-blur">
      <div className="flex h-16 items-center justify-between gap-4 px-5 lg:px-8">
        <Link href="/" className="flex items-center gap-3" aria-label="Semantix home">
          <img src="/seo/favicon.svg" alt="" className="size-8" aria-hidden="true" />
          <span className="font-mono text-lg font-semibold tracking-tight">semantix</span>
          <span className="hidden border-l border-border pl-3 text-sm text-muted-foreground sm:inline">Documentation</span>
        </Link>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={focusSearch}
            className="hidden h-9 items-center gap-3 rounded-md border border-border px-3 font-mono text-xs text-muted-foreground transition-colors hover:border-accent hover:text-foreground md:flex"
          >
            Search articles <span className="rounded bg-muted px-1.5 py-0.5 text-[10px]">Ctrl K</span>
          </button>
          <a
            href="https://github.com/Gnosil/semantix"
            target="_blank"
            rel="noopener noreferrer"
            className="rounded-md px-3 py-2 text-sm font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            GitHub ↗
          </a>
        </div>
      </div>
    </header>
  );
}
