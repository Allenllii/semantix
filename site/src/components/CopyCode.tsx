"use client";

import { Check, Copy } from "lucide-react";
import { useEffect, useRef, useState } from "react";

type CopyCodeProps = {
  code: string;
  className?: string;
  prompt?: boolean;
  tone?: "light" | "dark";
};

type CopyState = "idle" | "copied" | "failed";

function fallbackCopy(code: string) {
  const textarea = document.createElement("textarea");
  textarea.value = code;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();

  const copied = document.execCommand("copy");
  textarea.remove();

  if (!copied) {
    throw new Error("Copy command was rejected");
  }
}

export default function CopyCode({
  code,
  className = "",
  prompt = false,
  tone = "light",
}: CopyCodeProps) {
  const [state, setState] = useState<CopyState>("idle");
  const resetTimer = useRef<number | undefined>(undefined);
  const isDark = tone === "dark";

  useEffect(
    () => () => {
      if (resetTimer.current !== undefined) {
        window.clearTimeout(resetTimer.current);
      }
    },
    [],
  );

  async function handleCopy() {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(code);
      } else {
        fallbackCopy(code);
      }
      setState("copied");
    } catch {
      try {
        fallbackCopy(code);
        setState("copied");
      } catch {
        setState("failed");
      }
    }

    if (resetTimer.current !== undefined) {
      window.clearTimeout(resetTimer.current);
    }
    resetTimer.current = window.setTimeout(() => setState("idle"), 1800);
  }

  const label = state === "copied" ? "已复制" : state === "failed" ? "重试" : "复制";

  return (
    <div className={`relative ${className}`}>
      <pre
        className={`overflow-x-auto rounded-md p-3 pr-24 font-mono text-xs ${
          isDark
            ? "border border-white/10 bg-[oklch(0.21_0.006_260)] text-slate-300"
            : "border border-border/60 bg-[oklch(0.976_0.005_165)] text-[oklch(0.45_0.02_260)]"
        }`}
      >
        <code>{prompt ? `$ ${code}` : code}</code>
      </pre>
      <button
        type="button"
        onClick={handleCopy}
        aria-label={`${label}代码`}
        className={`absolute right-2.5 top-1/2 inline-flex h-8 -translate-y-1/2 items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 ${
          isDark
            ? "border-white/15 bg-[oklch(0.27_0.008_260)] text-slate-300 hover:border-emerald-400/60 hover:text-emerald-300"
            : "border-border/80 bg-white text-muted-foreground hover:border-accent/60 hover:text-accent"
        }`}
      >
        {state === "copied" ? (
          <Check aria-hidden="true" className="size-3.5" />
        ) : (
          <Copy aria-hidden="true" className="size-3.5" />
        )}
        <span aria-live="polite">{label}</span>
      </button>
    </div>
  );
}
