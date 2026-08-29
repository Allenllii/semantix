export type ContentAuthor = {
  name: string;
  url: string;
  profileUrl: string;
  contributionsUrl: string;
  description: string;
  focus: string;
  verifiedContributions: readonly {
    title: string;
    summary: string;
    url: string;
    date: string;
  }[];
};

export const contentAuthors: readonly ContentAuthor[] = [
  {
    name: "Gnosil",
    url: "https://github.com/Gnosil",
    profileUrl: "/authors/gnosil",
    contributionsUrl: "https://github.com/Gnosil/semantix/commits?author=Gnosil",
    description: "Repository maintainer working on the Semantix kernel, release packaging, verification, and security boundaries.",
    focus: "Kernel implementation, release decisions, verification gates, and repository maintenance.",
    verifiedContributions: [
      { title: "Full-product release bundle", summary: "Added cross-platform packaging, installation assets, checksums, and release documentation for the Semantix and Semantix bundle.", url: "https://github.com/Gnosil/semantix/commit/c65214cf63660c5fae428f910da280aee6b54233", date: "2026-08-12" },
      { title: "Usage and cost reporting", summary: "Implemented the usage recorder, pricing defaults, persisted evolution signal, CLI reporting, and package tests.", url: "https://github.com/Gnosil/semantix/commit/7882571935fcae5e59df58a10ba6d46315cc278f", date: "2026-08-11" },
      { title: "Judge input sanitization", summary: "Connected prompt sanitization to the judge path and added regression coverage for terminal escape sequences.", url: "https://github.com/Gnosil/semantix/commit/f0a01a107c9309a493a34724d1a06a4dd91da728", date: "2026-08-11" },
    ],
  },
  {
    name: "radianceded",
    url: "https://github.com/radianceded",
    profileUrl: "/authors/radianceded",
    contributionsUrl: "https://github.com/Gnosil/semantix/commits?author=radianceded",
    description: "Contributor maintaining the public website, evidence presentation, content provenance, and deployment fixes.",
    focus: "Website integration, evidence publishing, content attribution, and release verification.",
    verifiedContributions: [
      { title: "Citability evidence pages", summary: "Published evidence levels, reproducible run metadata, contributor attribution, and the public benchmark boundary.", url: "https://github.com/Gnosil/semantix/commit/b7f43ce33e80f217485079a9cc5b6b347e3233ce", date: "2026-08-13" },
      { title: "Static deployment metadata fix", summary: "Restored required Blog publication dates and added regression checks after the static build contract changed.", url: "https://github.com/Gnosil/semantix/commit/548dcacd62b01320dbb481bdb0ea980471bd4285", date: "2026-08-13" },
      { title: "Contributor route repair", summary: "Added the missing static contributor profiles and corrected the Allenllii identity links.", url: "https://github.com/Gnosil/semantix/commit/01c2a89615cb4abe39ba5abc45297adbdc9723db", date: "2026-08-13" },
    ],
  },
  {
    name: "jh10724-dotcom",
    url: "https://github.com/jh10724-dotcom",
    profileUrl: "/authors/jh10724-dotcom",
    contributionsUrl: "https://github.com/Gnosil/semantix/commits?author=jh10724-dotcom",
    description: "Contributor working on the Semantix landing experience and the repository's issue and pull-request intake workflow.",
    focus: "Homepage interaction, public product language, contributor intake, and repository workflow templates.",
    verifiedContributions: [
      { title: "Homepage copy and scroll behavior", summary: "Replaced an unverified self-evolution claim with verifiable memory-kernel language and shortened the intro scroll sequence.", url: "https://github.com/Gnosil/semantix/commit/c00127e25014c9a655de828d8f3f3f2187a67117", date: "2026-08-13" },
      { title: "Pull-request template", summary: "Added a repository pull-request template so proposed changes include scope and verification context.", url: "https://github.com/Gnosil/semantix/commit/49a0de42224fbe033ea813cc32d6fe780a3178d2", date: "2026-08-12" },
      { title: "Issue intake templates", summary: "Added and repaired bug, feature, and integration request templates for reproducible project intake.", url: "https://github.com/Gnosil/semantix/commit/f9904566f2501c0d994c2d7f65f0b86dd5816169", date: "2026-08-12" },
    ],
  },
  {
    name: "Allenllii",
    url: "https://github.com/Allenllii",
    profileUrl: "/authors/allenli1233",
    contributionsUrl: "https://github.com/Gnosil/semantix/commits?author=Allenllii",
    description: "Contributor improving website metadata, security disclosure, licensing consistency, and machine-readable project documentation.",
    focus: "Structured data, publication metadata, crawler documentation, licensing, and security disclosure.",
    verifiedContributions: [
      { title: "Blog publication metadata", summary: "Separated datePublished from dateModified and carried publication dates through parsing and structured data.", url: "https://github.com/Gnosil/semantix/commit/7684290d8104fc61535a23370536642973475a46", date: "2026-08-12" },
      { title: "Authoritative llms.txt", summary: "Reworked the crawler-facing index into an explicit description of current capabilities and evidence boundaries.", url: "https://github.com/Gnosil/semantix/commit/fab470e155eb5ff7c4170713c2bbd84d9bec2b17", date: "2026-08-12" },
      { title: "Security and license consistency", summary: "Completed RFC 9116 disclosure fields and aligned external license statements with the repository license.", url: "https://github.com/Gnosil/semantix/commit/b0e93539db482ef5bdae100a421b249858d66bef", date: "2026-08-12" },
    ],
  },
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
