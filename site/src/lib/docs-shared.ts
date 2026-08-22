export const documentSections = [
  "开始使用",
  "核心工作流",
  "理解机制",
  "集成与运维",
  "项目背景",
] as const;

export type DocumentSection = (typeof documentSections)[number];
