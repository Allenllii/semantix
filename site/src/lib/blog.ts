import "server-only";

import fs from "node:fs";
import path from "node:path";
import { BLOG_GROUPS, type BlogGroup, type BlogHeading, type BlogPostMeta } from "@/lib/blog-shared";

export { BLOG_GROUPS } from "@/lib/blog-shared";
export type { BlogHeading, BlogPostMeta } from "@/lib/blog-shared";

export type BlogPost = BlogPostMeta & {
  content: string;
  headings: BlogHeading[];
};

const BLOG_ROOT = path.resolve(process.cwd(), "../blog");

function parseFrontmatter(source: string, fileName: string) {
  const match = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n/);
  if (!match) throw new Error(`Missing frontmatter in blog/${fileName}`);

  const values = new Map<string, string>();
  for (const line of match[1].split(/\r?\n/)) {
    const separator = line.indexOf(":");
    if (separator === -1) throw new Error(`Invalid frontmatter in blog/${fileName}: ${line}`);
    values.set(
      line.slice(0, separator).trim(),
      line.slice(separator + 1).trim().replace(/^["']|["']$/g, "").replace(/\\"/g, '"'),
    );
  }

  const required = ["title", "description", "updated", "group", "order"];
  for (const key of required) {
    if (!values.get(key)) throw new Error(`Missing ${key} in blog/${fileName}`);
  }

  const group = values.get("group") as BlogGroup;
  if (!BLOG_GROUPS.includes(group)) throw new Error(`Invalid group in blog/${fileName}: ${group}`);

  const order = Number(values.get("order"));
  if (!Number.isInteger(order)) throw new Error(`Invalid order in blog/${fileName}`);

  return {
    title: values.get("title")!,
    description: values.get("description")!,
    updated: values.get("updated")!,
    group,
    order,
    content: source.slice(match[0].length),
  };
}

export function headingId(value: string) {
  return value
    .toLowerCase()
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/[`*_~]/g, "")
    .replace(/&/g, " and ")
    .replace(/[^a-z0-9\u00c0-\u024f\u4e00-\u9fff]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function extractHeadings(content: string): BlogHeading[] {
  return [...content.matchAll(/^(##|###)\s+(.+)$/gm)].map((match) => ({
    level: match[1].length as 2 | 3,
    text: match[2].replace(/\[([^\]]+)\]\([^)]*\)/g, "$1").replace(/[`*_~]/g, "").trim(),
    id: headingId(match[2]),
  }));
}

export function listBlogPosts(): BlogPostMeta[] {
  const posts = fs
    .readdirSync(BLOG_ROOT, { withFileTypes: true })
    .filter((entry) => entry.isFile() && entry.name.endsWith(".md"))
    .map((entry) => {
      const source = fs.readFileSync(path.join(BLOG_ROOT, entry.name), "utf8");
      const meta = parseFrontmatter(source, entry.name);
      return {
        slug: entry.name.replace(/\.md$/, ""),
        fileName: entry.name,
        title: meta.title,
        description: meta.description,
        updated: meta.updated,
        group: meta.group,
        order: meta.order,
      } satisfies BlogPostMeta;
    });

  const seen = new Set<string>();
  for (const post of posts) {
    if (seen.has(post.slug)) throw new Error(`Duplicate blog slug: ${post.slug}`);
    seen.add(post.slug);
  }

  return posts.sort(
    (a, b) => BLOG_GROUPS.indexOf(a.group) - BLOG_GROUPS.indexOf(b.group) || a.order - b.order,
  );
}

export function getBlogPost(slug: string) {
  return listBlogPosts().find((post) => post.slug === slug);
}

export function readBlogPost(post: BlogPostMeta): BlogPost {
  const source = fs.readFileSync(path.join(BLOG_ROOT, post.fileName), "utf8");
  const parsed = parseFrontmatter(source, post.fileName);
  return { ...post, content: parsed.content, headings: extractHeadings(parsed.content) };
}
