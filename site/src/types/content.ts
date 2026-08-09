// Content structures for the Semantix landing page.

export interface NavLink {
  label: string;
  labelEn: string;
  href: string;
}

export interface Feature {
  num: string;
  title: string;
  titleEn: string;
  body: string;
  code: string;
}

export interface Surface {
  title: string;
  titleEn: string;
  body: string;
  code: string;
}

export interface Step {
  num: string;
  title: string;
  titleEn: string;
  body: string;
  code: string;
}

export interface TermLine {
  prompt?: boolean;
  cmd?: string;
  arg?: string;
  out?: string;
  dim?: string;
}

export interface Contributor {
  login: string;
  href: string;
  initials: string;
}
