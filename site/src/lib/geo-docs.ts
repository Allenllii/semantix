import { readFile } from "node:fs/promises";
import path from "node:path";

export type GeoDocument = {
  slug: string;
  fileName: string;
  title: string;
  description: string;
  language: "中文" | "English";
  depth: "速览" | "深入";
  lastUpdated: string;
};

export const geoDocuments: readonly GeoDocument[] = [
  {
    slug: "profile",
    fileName: "overview.md",
    title: "Semantix 项目速览",
    description: "面向开发者的中文项目定位、术语与进度概览。",
    language: "中文",
    depth: "速览",
    lastUpdated: "2026-08-10",
  },
  {
    slug: "profile-en",
    fileName: "overview.en.md",
    title: "Semantix Project Overview",
    description: "A concise English overview of the project's purpose, terminology, and progress.",
    language: "English",
    depth: "速览",
    lastUpdated: "2026-08-10",
  },
  {
    slug: "guide",
    fileName: "deep-dive.md",
    title: "从零理解 Semantix",
    description: "从第一性原理出发的中文深度解读。",
    language: "中文",
    depth: "深入",
    lastUpdated: "2026-08-10",
  },
  {
    slug: "guide-en",
    fileName: "deep-dive.en.md",
    title: "Understanding Semantix from Scratch",
    description: "An English deep dive from first principles.",
    language: "English",
    depth: "深入",
    lastUpdated: "2026-08-10",
  },
];

export function getGeoDocument(slug: string) {
  return geoDocuments.find((document) => document.slug === slug);
}

export async function readGeoDocument(document: GeoDocument) {
  const filePath = path.join(process.cwd(), "content", "geo", document.fileName);
  return readFile(filePath, "utf8");
}
