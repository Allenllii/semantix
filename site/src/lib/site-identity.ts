export const siteIdentity = {
  productName: "Semantix",
  productUrl: "https://semantix.ensureok.ai",
  repositoryUrl: "https://github.com/Gnosil/semantix",
  licenseName: "FSL-1.1-MIT",
  lastUpdated: "2026-08-10",
  operator: {
    legalName: "确石人工智能科技（上海）有限公司",
    brandName: "确石智能",
    englishName: "Queshi Intelligence",
    url: "https://www.ensureok.ai/",
    logoUrl: "https://www.ensureok.ai/ensureok-logo.png",
    email: "junhaihuang@aiqueshi.com",
  },
} as const;

export const organizationJsonLd = {
  "@context": "https://schema.org",
  "@type": "Organization",
  "@id": `${siteIdentity.operator.url}#organization`,
  name: siteIdentity.operator.legalName,
  alternateName: [
    siteIdentity.operator.brandName,
    siteIdentity.operator.englishName,
  ],
  url: siteIdentity.operator.url,
  logo: siteIdentity.operator.logoUrl,
  email: `mailto:${siteIdentity.operator.email}`,
  sameAs: [siteIdentity.repositoryUrl],
  contactPoint: {
    "@type": "ContactPoint",
    email: siteIdentity.operator.email,
    contactType: "project inquiries",
    availableLanguage: ["zh-CN", "en"],
  },
};
