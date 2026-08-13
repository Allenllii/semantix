export type EvidenceLevel = "E0" | "E1" | "E2" | "E3";

export type EvidenceRun = {
  id: string;
  label: string;
  level: EvidenceLevel;
  date: string;
  commit: string;
  environment: string;
  command: string;
  summary: string;
  passed: string[];
  failed: string[];
  limitations: string[];
  productionBenchmark: boolean;
};

export type EvidenceArtifact = {
  id: string;
  title: string;
  level: EvidenceLevel;
  purpose: string;
  input: string;
  command: string;
  output: string;
  interpretation: string;
  limitations: string[];
  sourceUrl: string;
};

export const evidenceRuns: readonly EvidenceRun[] = [
  {
    id: "semantix-2026-08-12-windows",
    label: "Windows package run",
    level: "E1",
    date: "2026-08-12",
    commit: "e93668ef919919d4a74dabfca0de7fbf7a0f0e65",
    environment: "Go 1.26.5 · Windows/amd64",
    command: "go test -count=1 ./...",
    summary:
      "Core retrieval, cache, ingestion, injection, evolve, usage, and zone packages passed. Two Unix-style file permission assertions failed on Windows.",
    passed: [
      "semantix/kernel/bm25",
      "semantix/kernel/cache",
      "semantix/kernel/ingest",
      "semantix/kernel/inject",
      "semantix/kernel/evolve",
      "semantix/kernel/usage",
      "semantix/kernel/zone",
    ],
    failed: [
      "semantix/kernel/slice: store file perm = 666, want 600",
      "semantix/cmd/semantix: evolve state perms = 666, want 600",
    ],
    limitations: [
      "Repository fixtures are not an independent production dataset.",
      "The run does not establish real-session relevance or cost reduction.",
      "Windows ACL behavior is not equivalent to Unix file mode bits.",
    ],
    productionBenchmark: false,
  },
] as const;

export const defaultEvidenceRun = evidenceRuns[0];

export const evidenceArtifacts: readonly EvidenceArtifact[] = [
  {
    id: "m0-gate-eval-greyzone",
    title: "Single-threshold versus three-region retrieval",
    level: "E2",
    purpose: "Compare a single BM25 reuse threshold with the conservative hit/grey/miss policy on the repository's labeled oracle fixture.",
    input: "cmd/semantix/testdata/eval-greyzone.tsv · SHA-256 83963A04C27E3949A53FCA5C5690D46242017D2C64658848C67032773B0F1E08",
    command: "go run ./cmd/semantix eval --set cmd/semantix/testdata/eval-greyzone.tsv --train-frac 0.7 --single-tau 1.0 --grey-target 30",
    output: "15 rows · 10 train / 5 replay · single reuse 100.0% with 20.0% error · three-region reuse 60.0% with 0.0% error · verdict PASS · grey ratio 40.0% (above the 30.0% alarm)",
    interpretation: "On this small fixture, the three-region policy trades reuse volume for zero observed false reuse. The grey-zone alarm remains visible and is not presented as a tuned production result.",
    limitations: [
      "The rows are a repository fixture, not a real-session corpus.",
      "Five replay rows are too few for a general relevance claim.",
      "The command reports an alarm because the fixture's grey ratio exceeds the default target.",
    ],
    sourceUrl: "https://github.com/Gnosil/semantix/blob/main/cmd/semantix/testdata/eval-greyzone.tsv",
  },
];

export const defaultEvidenceArtifact = evidenceArtifacts[0];

const auditedBlogSlugs = new Set([
  "turning-tool-calls-searchable-semantic-slices",
  "semantic-kernel-smarter-tool-execution",
  "kernel-alternative-custom-agent-scheduling",
  "go-native-semantic-memory-flexible-stack",
  "agent-kernel-tool-call-history-semantic-slices",
  "go-agent-framework-neutral-memory-layer",
  "go-native-framework-agnostic-memory-kernel",
  "semantic-cache-kernel-coding-agents",
]);

export function getBlogEvidence(slug: string) {
  return {
    level: auditedBlogSlugs.has(slug) ? ("E1" as const) : ("E0" as const),
    run: auditedBlogSlugs.has(slug) ? defaultEvidenceRun : null,
  };
}

export function isAuditedBlogSlug(slug: string) {
  return auditedBlogSlugs.has(slug);
}

export function evidenceLevelLabel(level: EvidenceLevel) {
  return {
    E0: "E0 · design or editorial context",
    E1: "E1 · repository tests",
    E2: "E2 · reproducible synthetic run",
    E3: "E3 · independent or real-session reproduction",
  }[level];
}
