import type { BlogHeading } from "@/lib/blog-shared";

export default function BlogTableOfContents({ headings }: { headings: BlogHeading[] }) {
  return (
    <aside className="hidden xl:block">
      <nav aria-label="On this page" className="sticky top-24 border-l border-border pl-5">
        <p className="mb-4 text-sm font-semibold text-foreground">On this page</p>
        <ol className="space-y-2.5">
          {headings.map((heading) => (
            <li key={`${heading.level}-${heading.id}`} className={heading.level === 3 ? "pl-3" : undefined}>
              <a href={`#${heading.id}`} className="block text-xs leading-5 text-muted-foreground transition-colors hover:text-accent">
                {heading.text}
              </a>
            </li>
          ))}
        </ol>
      </nav>
    </aside>
  );
}
