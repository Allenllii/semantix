export type ContentAuthor = {
  name: string;
  url: string;
};

export const contentAuthors: readonly ContentAuthor[] = [
  { name: "Gnosil", url: "https://github.com/Gnosil" },
  { name: "radianceded", url: "https://github.com/radianceded" },
  { name: "jh10724-dotcom", url: "https://github.com/jh10724-dotcom" },
  { name: "Allenli1233", url: "https://github.com/Allenli1233" },
] as const;

/** Stable distribution: a page keeps the same verifiable author across builds. */
export function getContentAuthor(pageKey: string): ContentAuthor {
  const hash = [...pageKey].reduce(
    (value, character) => (value * 31 + character.codePointAt(0)!) >>> 0,
    0,
  );

  return contentAuthors[hash % contentAuthors.length];
}

export function personJsonLd(author: ContentAuthor) {
  return {
    "@type": "Person",
    "@id": `${author.url}#person`,
    name: author.name,
    url: author.url,
    sameAs: [author.url],
  };
}

export const maintainersJsonLd = {
  "@context": "https://schema.org",
  "@graph": contentAuthors.map(personJsonLd),
};
