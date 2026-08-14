import type { Metadata } from "next";
import {
  organizationJsonLd,
  siteIdentity,
  softwareApplicationJsonLd,
  websiteJsonLd,
} from "@/lib/site-identity";
import { maintainersJsonLd } from "@/lib/content-authors";
import "@fontsource-variable/inter/wght.css";
import "@fontsource-variable/jetbrains-mono/wght.css";
import "@fontsource-variable/noto-sans-sc/wght.css";
import "./globals.css";

export const metadata: Metadata = {
  metadataBase: new URL(siteIdentity.productUrl),
  title: "Semantix - a verifiable memory kernel for agents",
  description:
    "An open-source Go memory kernel with semantic slice extraction, BM25 retrieval, stable injection, and explicit experimental boundaries.",
  alternates: { canonical: "/" },
  icons: [{ rel: "icon", url: "/seo/favicon.svg", type: "image/svg+xml" }],
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN" className="h-full antialiased">
      <body className="min-h-full flex flex-col">
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{
            __html: JSON.stringify(organizationJsonLd).replace(/</g, "\\u003c"),
          }}
        />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{
            __html: JSON.stringify(websiteJsonLd).replace(/</g, "\\u003c"),
          }}
        />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{
            __html: JSON.stringify(softwareApplicationJsonLd).replace(/</g, "\\u003c"),
          }}
        />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{
            __html: JSON.stringify(maintainersJsonLd).replace(/</g, "\\u003c"),
          }}
        />
        {children}
      </body>
    </html>
  );
}
