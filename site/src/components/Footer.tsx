import Link from "next/link";

const links = [
  { label: "Docs", href: "/docs", external: false },
  { label: "GitHub", href: "https://github.com/Gnosil/semantix", external: true },
  {
    label: "README",
    href: "https://github.com/Gnosil/semantix/blob/main/README.md",
    external: true,
  },
  {
    label: "GitHub Docs",
    href: "https://github.com/Gnosil/semantix/tree/main/docs",
    external: true,
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
            <Link
              key={link.label}
              href={link.href}
              target={link.external ? "_blank" : undefined}
              rel={link.external ? "noopener noreferrer" : undefined}
              className="text-sm text-muted-foreground transition-colors hover:text-accent"
            >
              {link.label}
            </Link>
          ))}
        </nav>
      </div>
    </footer>
  );
}
