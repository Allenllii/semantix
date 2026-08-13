export type ContentAuthor = {
  name: string;
  url: string;
  profileUrl: string;
  contributionsUrl: string;
  description: string;
};

export const contentAuthors: readonly ContentAuthor[] = [
  { name: "Gnosil", url: "https://github.com/Gnosil", profileUrl: "/authors/gnosil", contributionsUrl: "https://github.com/Gnosil/semantix/commits?author=Gnosil", description: "Semantix repository maintainer; project work is traceable through GitHub." },
  { name: "radianceded", url: "https://github.com/radianceded", profileUrl: "/authors/radianceded", contributionsUrl: "https://github.com/Gnosil/semantix/commits?author=radianceded", description: "Semantix contributor; project work is traceable through GitHub." },
  { name: "jh10724-dotcom", url: "https://github.com/jh10724-dotcom", profileUrl: "/authors/jh10724-dotcom", contributionsUrl: "https://github.com/Gnosil/semantix/commits?author=jh10724-dotcom", description: "Semantix contributor; project work is traceable through GitHub." },
  { name: "Allenli1233", url: "https://github.com/Allenli1233", profileUrl: "/authors/allenli1233", contributionsUrl: "https://github.com/Gnosil/semantix/commits?author=Allenli1233", description: "Semantix contributor; project work is traceable through GitHub." },
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
    description: author.description,
  };
}

export const maintainersJsonLd = {
  "@context": "https://schema.org",
  "@graph": contentAuthors.map(personJsonLd),
};
