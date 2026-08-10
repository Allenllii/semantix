import type { Metadata } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import "./globals.css";

const inter = Inter({
  variable: "--font-inter",
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
});

const jetbrains = JetBrains_Mono({
  variable: "--font-jetbrains",
  subsets: ["latin"],
  weight: ["400", "500", "600"],
});

export const metadata: Metadata = {
  title: "Semantix — a self-evolving agent kernel",
  description:
    "A self-evolving agent kernel that sits between any agent harness and its resources — semantic caching, speculative prefetch, and scheduling that learn from your usage habits.",
  icons: [{ rel: "icon", url: "/seo/favicon.svg", type: "image/svg+xml" }],
};

// Organization JSON-LD（GEO Week 1：#4 No Organization schema found）
// sameAs 仅列已核实真实存在的链接；其余账号待补充（见 issue，不编造）
const organizationSchema = {
  "@context": "https://schema.org",
  "@type": "Organization",
  name: "Semantix",
  url: "https://semantix.ensureok.ai",
  logo: "https://semantix.ensureok.ai/seo/favicon.svg",
  sameAs: ["https://github.com/Gnosil/semantix"],
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="zh-CN"
      className={`${inter.variable} ${jetbrains.variable} h-full antialiased`}
    >
      <head>
        <script
          type="application/ld+json"
          // 静态常量序列化，无用户输入
          dangerouslySetInnerHTML={{ __html: JSON.stringify(organizationSchema) }}
        />
      </head>
      <body className="min-h-full flex flex-col">{children}</body>
    </html>
  );
}
