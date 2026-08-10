import Footer from "@/components/Footer";
import GeoScrollReset from "@/components/GeoScrollReset";
import Nav from "@/components/Nav";

export default function GeoLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <>
      <Nav />
      <GeoScrollReset />
      {children}
      <Footer />
    </>
  );
}
