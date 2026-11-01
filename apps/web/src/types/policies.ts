// The terms of service and privacy policy as the policy pages render them:
// typed blocks made at build time from the Markdown in content/policy/
// (lib/policies.server.ts), so the browser never parses Markdown and no
// HTML from a file ever reaches the page.

// The documents, by their file and URL name.
export const policyNames = ["terms-of-service", "privacy-policy"] as const;
export type PolicyName = (typeof policyNames)[number];

// Text inside a block. Text is plain: React escapes it.
export type Inline =
  | { type: "text"; text: string }
  | { type: "strong"; children: Inline[] }
  | { type: "em"; children: Inline[] }
  | { type: "code"; text: string }
  // href is https:, mailto: or a path on this site, checked at build time.
  | { type: "link"; href: string; children: Inline[] }
  | { type: "break" };

export type Block =
  // level 2–4; id, from its text, so a section can be linked (#cookies).
  | { type: "heading"; level: 2 | 3 | 4; id: string; children: Inline[] }
  | { type: "paragraph"; children: Inline[] }
  // Each item is its own blocks: its text, and any nested list.
  | { type: "list"; ordered: boolean; items: Block[][] }
  | { type: "table"; header: Inline[][]; rows: Inline[][][] }
  | { type: "rule" };

// One document: its front matter and its blocks.
export type Policy = {
  title: string;
  description: string;
  // The version, YYYY-MM-DD, shown as "Last updated".
  updated: string;
  // Still the committed template: production refuses to publish it.
  boilerplate: boolean;
  blocks: Block[];
};

// The versions of the two documents (their updated dates).
export type PolicyVersions = { termsOfService: string; privacyPolicy: string };

// What GET /api/policies answers.
export type PolicyStatus =
  { enabled: false } | ({ enabled: true } & PolicyVersions);

// What an account agreed to at sign-up, and when (the user's `policies`).
export type PolicyAgreement = PolicyVersions & { acceptedAt: string };
