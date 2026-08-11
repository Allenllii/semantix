"use client";

type BlogSearchProps = {
  value: string;
  onChange: (value: string) => void;
};

export default function BlogSearch({ value, onChange }: BlogSearchProps) {
  return (
    <label className="relative block">
      <span className="sr-only">Search articles</span>
      <input
        id="blog-search"
        type="search"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder="Search articles"
        className="h-10 w-full rounded-md border border-border bg-white px-3 pr-14 font-mono text-xs text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-accent focus:ring-2 focus:ring-accent/10"
      />
      <span className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 rounded border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
        Ctrl K
      </span>
    </label>
  );
}
