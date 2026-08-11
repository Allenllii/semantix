export const BLOG_GROUPS = [
  "Evaluation Guides",
  "Semantic Slices",
  "Semantic Cache",
  "Scheduling & Harness",
  "Go & Framework Independence",
] as const;

export type BlogGroup = (typeof BLOG_GROUPS)[number];

export type BlogPostMeta = {
  slug: string;
  title: string;
  description: string;
  updated: string;
  group: BlogGroup;
  order: number;
  fileName: string;
};

export type BlogHeading = {
  id: string;
  text: string;
  level: 2 | 3;
};
