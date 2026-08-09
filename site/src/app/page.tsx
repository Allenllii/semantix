import Nav from "@/components/Nav";
import Hero from "@/components/Hero";
import Features from "@/components/Features";
import Components from "@/components/Components";
import Roadmap from "@/components/Roadmap";
import Community from "@/components/Community";
import Install from "@/components/Install";
import Footer from "@/components/Footer";

export default function Home() {
  return (
    <>
      <Nav />
      <main>
        <Hero />
        <Features />
        <Components />
        <Roadmap />
        <Community />
        <Install />
      </main>
      <Footer />
    </>
  );
}
