import type { MetadataRoute } from "next";
import { siteIdentity } from "@/lib/site-identity";
import { geoDocuments } from "@/lib/geo-docs";
import { getBlogPosts } from "@/lib/blog-posts";

const BASE = siteIdentity.productUrl;

// output: "export" 下 sitemap route 必须显式静态化
export const dynamic = "force-static";

// 站点级入口 + 全部静态 docs 与 blog 页面。
// lastModified 使用站点身份配置和内容 frontmatter，不随每次构建变化。
export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const lastModified = new Date(siteIdentity.lastUpdated);
  const blogPosts = await getBlogPosts();
  return [
    { url: `${BASE}/`, lastModified, changeFrequency: "weekly", priority: 1 },
    {
      url: `${BASE}/about`,
      lastModified,
      changeFrequency: "monthly",
      priority: 0.8,
    },
    {
      url: `${BASE}/docs`,
      lastModified,
      changeFrequency: "weekly",
      priority: 0.9,
    },
    ...geoDocuments.map((document) => ({
      url: `${BASE}/docs/${document.slug}`,
      lastModified: new Date(document.lastUpdated),
      changeFrequency: "weekly" as const,
      priority: 0.8,
    })),
    {
      url: `${BASE}/docs/faq`,
      lastModified,
      changeFrequency: "monthly",
      priority: 0.7,
    },
    {
      url: `${BASE}/blog`,
      lastModified,
      changeFrequency: "weekly",
      priority: 0.8,
    },
    ...blogPosts.map((post) => ({
      url: `${BASE}/blog/${post.slug}`,
      lastModified: new Date(post.updated),
      changeFrequency: "monthly" as const,
      priority: 0.7,
    })),
    {
      url: `${BASE}/terms`,
      lastModified,
      changeFrequency: "yearly",
      priority: 0.3,
    },
    {
      url: `${BASE}/privacy`,
      lastModified,
      changeFrequency: "yearly",
      priority: 0.3,
    },
  ];
}
