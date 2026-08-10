import { readFile } from "node:fs/promises";
import path from "node:path";

export type GeoDocument = {
  slug: string;
  fileName: string;
  title: string;
  description: string;
  language: "中文" | "English";
  depth: "速览" | "深入";
};

export const geoDocuments: readonly GeoDocument[] = [
  {
    slug: "profile",
    fileName: "GEO.md",
    title: "Semantix 项目语义档案",
    description: "面向开发者与 AI 引擎的中文项目速览。",
    language: "中文",
    depth: "速览",
  },
  {
    slug: "profile-en",
    fileName: "GEO.en.md",
    title: "Semantix Project Semantic Profile",
    description: "A concise English project profile for developers and AI engines.",
    language: "English",
    depth: "速览",
  },
  {
    slug: "guide",
    fileName: "GEO-guide.md",
    title: "从零理解 Semantix",
    description: "从第一性原理出发的中文深度解读。",
    language: "中文",
    depth: "深入",
  },
  {
    slug: "guide-en",
    fileName: "GEO-guide.en.md",
    title: "Understanding Semantix from Scratch",
    description: "An English deep dive from first principles.",
    language: "English",
    depth: "深入",
  },
];

export function getGeoDocument(slug: string) {
  return geoDocuments.find((document) => document.slug === slug);
}

export async function readGeoDocument(document: GeoDocument) {
  const filePath = path.join(process.cwd(), "content", "geo", document.fileName);
  return readFile(filePath, "utf8");
}
