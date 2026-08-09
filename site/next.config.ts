import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  /* Static export: this is a pure landing page with no API routes or
     dynamic data; Cloudflare Pages / any static host can serve it. */
  output: "export",
  images: { unoptimized: true },
};

export default nextConfig;
