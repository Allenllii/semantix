import type { MetadataRoute } from "next";
import { siteIdentity } from "@/lib/site-identity";
import { geoDocuments } from "@/lib/geo-docs";

const BASE = siteIdentity.productUrl;

// output: "export" 下 sitemap route 必须显式静态化
export const dynamic = "force-static";

// 站点级入口（/、/about、/docs、/terms）+ 全部静态 docs 页面。
// lastModified 使用站点统一身份配置（site-identity.ts / geo-docs.ts），不随每次构建变化。
export default function sitemap(): MetadataRoute.Sitemap {
  const lastModified = new Date(siteIdentity.lastUpdated);
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
