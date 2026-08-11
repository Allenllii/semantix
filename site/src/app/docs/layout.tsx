import DocsSidebar from "@/components/DocsSidebar";
import GeoScrollReset from "@/components/GeoScrollReset";
import Nav from "@/components/Nav";
import { geoDocuments } from "@/lib/geo-docs";

export default function GeoLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <>
      <Nav />
      <GeoScrollReset />
      <main className="min-h-[100dvh] bg-background pt-16">
        <div className="mx-auto grid min-h-[calc(100dvh-4rem)] w-full max-w-[1440px] lg:grid-cols-[280px_minmax(0,1fr)]">
          <DocsSidebar documents={geoDocuments} />
          <div className="min-w-0 lg:border-l lg:border-border">{children}</div>
        </div>
      </main>
    </>
  );
}
