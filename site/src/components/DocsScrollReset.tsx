"use client";

import { useEffect } from "react";
import { usePathname } from "next/navigation";

export default function DocsScrollReset() {
  const pathname = usePathname();

  useEffect(() => {
    window.scrollTo({ top: 0, left: 0, behavior: "auto" });
  }, [pathname]);

  return null;
}
