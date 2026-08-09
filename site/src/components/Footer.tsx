const links = [
  { label: "GitHub", href: "https://github.com/Gnosil/semantix" },
  {
    label: "README",
    href: "https://github.com/Gnosil/semantix/blob/main/README.md",
  },
  {
    label: "Docs",
    href: "https://github.com/Gnosil/semantix/tree/main/docs",
  },
] as const;

export default function Footer() {
  return (
    <footer className="border-t border-border bg-muted py-8">
      <div className="wrap flex flex-col items-center justify-between gap-4 md:flex-row">
        <p className="text-sm text-muted-foreground font-mono">
          Semantix © 2026 · MIT licensed · built to evolve.
        </p>
        <nav className="flex items-center gap-6">
          {links.map((link) => (
            <a
              key={link.label}
              href={link.href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-muted-foreground transition-colors hover:text-accent"
            >
              {link.label}
            </a>
          ))}
        </nav>
      </div>
    </footer>
  );
}
