import type { MetadataRoute } from "next";
import { siteIdentity } from "@/lib/site-identity";

export default function sitemap(): MetadataRoute.Sitemap {
  const base = siteIdentity.productUrl;
  const lastModified = new Date(siteIdentity.lastUpdated);
  return [
    {
      url: `${base}/`,
      lastModified,
      changeFrequency: "weekly",
      priority: 1,
    },
    {
      url: `${base}/about`,
      lastModified,
      changeFrequency: "monthly",
      priority: 0.8,
    },
    {
      url: `${base}/docs`,
      lastModified,
      changeFrequency: "weekly",
      priority: 0.9,
    },
    {
      url: `${base}/terms`,
      lastModified,
      changeFrequency: "yearly",
      priority: 0.3,
    },
  ];
}
