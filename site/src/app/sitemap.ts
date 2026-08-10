import type { MetadataRoute } from "next";
import { siteIdentity } from "@/lib/site-identity";

const BASE = siteIdentity.productUrl;

// output: "export" 下 sitemap route 必须显式静态化
export const dynamic = "force-static";

// 站点级入口（/、/about、/docs、/terms）。
// 具体文档 URL 由 docs 渲染层（PR #23 的 /docs 固定页方案）负责，
// 本文件只声明站点级入口，避免与任何 docs 路由实现耦合。
// lastModified 使用站点统一身份配置（site-identity.ts），不随每次构建变化。
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
