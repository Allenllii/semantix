import Nav from "@/components/Nav";
import Hero from "@/components/Hero";
import Features from "@/components/Features";
import Components from "@/components/Components";
import Roadmap from "@/components/Roadmap";
import Community from "@/components/Community";
import Install from "@/components/Install";
import Faq, { faqItems } from "@/components/Faq";
import Footer from "@/components/Footer";
import { siteIdentity } from "@/lib/site-identity";

const webpageJsonLd = {
  "@context": "https://schema.org",
  "@type": "WebPage",
  "@id": `${siteIdentity.productUrl}/#webpage`,
  url: `${siteIdentity.productUrl}/`,
  name: `${siteIdentity.productName} - a self-evolving agent kernel`,
  dateModified: siteIdentity.lastUpdated,
};

const faqJsonLd = {
  "@context": "https://schema.org",
  "@type": "FAQPage",
  "@id": `${siteIdentity.productUrl}/#faq`,
  mainEntity: faqItems.map((item) => ({
    "@type": "Question",
    name: item.question,
    acceptedAnswer: {
      "@type": "Answer",
      text: item.answer,
    },
  })),
};

export default function Home() {
  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify(webpageJsonLd).replace(/</g, "\\u003c"),
        }}
      />
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify(faqJsonLd).replace(/</g, "\\u003c"),
        }}
      />
      <Nav />
      <main>
        <Hero />
        <Features />
        <Components />
        <Roadmap />
        <Community />
        <Install />
        <Faq />
      </main>
      <Footer />
    </>
  );
}
