import { readFile, readdir } from "node:fs/promises";
import path from "node:path";

export type BlogPost = {
  slug: string;
  title: string;
  updated: string;
  order: number;
  excerpt: string;
  content: string;
};

const blogDirectory = path.join(process.cwd(), "..", "docs", "blog");

function readFrontmatter(source: string) {
  const match = source.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n/);
  if (!match) throw new Error("Blog post is missing frontmatter");

  const values = Object.fromEntries(
    match[1]
      .split(/\r?\n/)
      .map((line) => line.match(/^([a-zA-Z]+):\s*(.+)$/))
      .filter((entry): entry is RegExpMatchArray => Boolean(entry))
      .map((entry) => [entry[1], entry[2].replace(/^['\"]|['\"]$/g, "")]),
  );

  return { values, body: source.slice(match[0].length) };
}

function createExcerpt(body: string) {
  const paragraph = body
    .replace(/^# .+\r?\n+/, "")
    .split(/\r?\n\r?\n/)
    .find((block) => block.trim() && !block.trimStart().startsWith("#"));

  return normalizeVisibleText((paragraph ?? "").replace(/\s+/g, " ").trim());
}

function normalizeVisibleText(value: string) {
  return value.replace(/\s*—\s*/g, " - ").replaceAll("–", "-");
}

async function readBlogPost(fileName: string): Promise<BlogPost> {
  const source = await readFile(path.join(blogDirectory, fileName), "utf8");
  const { values, body } = readFrontmatter(source);

  if (!values.title || !values.updated || !values.order) {
    throw new Error(`${fileName} has incomplete blog frontmatter`);
  }

  return {
    slug: fileName.replace(/\.md$/, ""),
    title: values.title,
    updated: values.updated,
    order: Number(values.order),
    excerpt: createExcerpt(body),
    content: normalizeVisibleText(body),
  };
}

export async function getBlogPosts() {
  const files = (await readdir(blogDirectory)).filter((file) => file.endsWith(".md"));
  const posts = await Promise.all(files.map(readBlogPost));
  return posts.sort((left, right) => left.order - right.order);
}

export async function getBlogPost(slug: string) {
  const posts = await getBlogPosts();
  return posts.find((post) => post.slug === slug);
}
