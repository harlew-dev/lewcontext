import { readFileSync } from "node:fs";
import path from "node:path";
import { marked, type Token, type Tokens } from "marked";
import type { Block, Inline, Policy, PolicyName } from "@/types/policies";
// Reads a policy document (content/policy/<name>.md) into typed blocks, at
// build time only: the policy pages' loaders run while prerendering, and
// the app build's manifest plugin (vite.config.ts) reads the same files.
// The .server suffix keeps it, `marked` and node:fs out of every browser
// bundle. Anything the pages can't show safely fails the build with the
// file and line, so a mistake is found before deploying.

// Where the documents live: content/policy under apps/web, which is where
// every build and test runs.
export function policyFile(name: PolicyName, root = process.cwd()): string {
  return path.join(root, "content", "policy", `${name}.md`);
}

// readPolicy reads and parses one document.
export function readPolicy(name: PolicyName, root?: string): Policy {
  const file = policyFile(name, root);
  return parsePolicy(
    readFileSync(file, "utf8"),
    path.relative(process.cwd(), file),
  );
}

// A Markdown mistake, reported with where it is.
class PolicyError extends Error {
  constructor(file: string, line: number, message: string) {
    super(`${file}:${line}: ${message}`);
    this.name = "PolicyError";
  }
}

const frontMatterKeys = new Set([
  "title",
  "description",
  "updated",
  "boilerplate",
]);

// parsePolicy turns a document's text (front matter, then Markdown) into a
// Policy. `file` names it in errors.
export function parsePolicy(source: string, file: string): Policy {
  const text = source.replace(/\r\n?/g, "\n");
  const match = /^---\n([\s\S]*?)\n---\n/.exec(text);
  if (!match)
    throw new PolicyError(
      file,
      1,
      "start with a front matter block between --- lines",
    );
  const meta = frontMatter(match[1], file);
  const offset = match[0].split("\n").length - 1;
  const body = text.slice(match[0].length);
  return { ...meta, blocks: blocks(marked.lexer(body), body, file, offset) };
}

function frontMatter(block: string, file: string) {
  const values: Record<string, string> = {};
  block.split("\n").forEach((line, i) => {
    if (line.trim() === "") return;
    const m = /^([a-z]+):\s*(.*)$/.exec(line);
    if (!m || !frontMatterKeys.has(m[1]))
      throw new PolicyError(
        file,
        i + 2,
        `unknown front matter "${line}" (use ${[...frontMatterKeys].join(", ")})`,
      );
    values[m[1]] = m[2].trim();
  });
  if (!values.title)
    throw new PolicyError(file, 1, "front matter needs a title");
  if (!values.updated || !realDate(values.updated))
    throw new PolicyError(
      file,
      1,
      "front matter needs updated: a real YYYY-MM-DD date",
    );
  if (
    values.boilerplate !== undefined &&
    !["true", "false"].includes(values.boilerplate)
  )
    throw new PolicyError(file, 1, "boilerplate is true or false");
  return {
    title: values.title,
    description: values.description ?? "",
    updated: values.updated,
    boilerplate: values.boilerplate === "true",
  };
}

function realDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const date = new Date(`${value}T00:00:00Z`);
  return (
    !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value
  );
}

// The line each top-level token starts on, for errors.
function blocks(
  tokens: Token[],
  body: string,
  file: string,
  offset: number,
): Block[] {
  const ids = new Map<string, number>();
  const out: Block[] = [];
  let position = 0;
  for (const token of tokens) {
    const start = body.indexOf(token.raw, position);
    const line =
      offset + 1 + body.slice(0, Math.max(start, 0)).split("\n").length - 1;
    position = start >= 0 ? start + token.raw.length : position;
    const block = toBlock(token, { file, line, ids });
    if (block) out.push(block);
  }
  return out;
}

type Where = { file: string; line: number; ids: Map<string, number> };

function toBlock(token: Token, at: Where): Block | null {
  switch (token.type) {
    case "space":
      return null;
    case "heading": {
      const heading = token as Tokens.Heading;
      if (heading.depth < 2 || heading.depth > 4)
        throw new PolicyError(
          at.file,
          at.line,
          "use headings ## to #### (the title comes from the front matter)",
        );
      const children = inlines(heading.tokens, at);
      return {
        type: "heading",
        level: heading.depth as 2 | 3 | 4,
        id: headingId(plain(children), at.ids),
        children,
      };
    }
    case "paragraph":
      return {
        type: "paragraph",
        children: inlines((token as Tokens.Paragraph).tokens, at),
      };
    case "text": {
      // A tight list item's text.
      const text = token as Tokens.Text;
      return {
        type: "paragraph",
        children: text.tokens
          ? inlines(text.tokens, at)
          : [{ type: "text", text: decode(text.text) }],
      };
    }
    case "list": {
      const list = token as Tokens.List;
      return {
        type: "list",
        ordered: list.ordered,
        items: list.items.map((item) => {
          if (item.task)
            throw new PolicyError(
              at.file,
              at.line,
              "task lists aren't allowed",
            );
          return item.tokens
            .map((t) => toBlock(t, at))
            .filter((b): b is Block => b !== null);
        }),
      };
    }
    case "table": {
      const table = token as Tokens.Table;
      return {
        type: "table",
        header: table.header.map((cell) => inlines(cell.tokens, at)),
        rows: table.rows.map((row) =>
          row.map((cell) => inlines(cell.tokens, at)),
        ),
      };
    }
    case "hr":
      return { type: "rule" };
    case "html":
      throw new PolicyError(at.file, at.line, "raw HTML isn't allowed");
    default:
      throw new PolicyError(
        at.file,
        at.line,
        `${describe(token.type)} isn't allowed`,
      );
  }
}

function inlines(tokens: Token[] | undefined, at: Where): Inline[] {
  // A text token in a list item can hold inline tokens of its own: they
  // take its place.
  return (tokens ?? []).flatMap((token) => {
    const nested =
      token.type === "text" ? (token as Tokens.Text).tokens : undefined;
    return nested && nested.length > 0
      ? inlines(nested, at)
      : [inline(token, at)];
  });
}

function inline(token: Token, at: Where): Inline {
  switch (token.type) {
    case "text":
      return { type: "text", text: decode((token as Tokens.Text).text) };
    case "escape":
      return { type: "text", text: decode((token as Tokens.Escape).text) };
    case "strong":
      return {
        type: "strong",
        children: inlines((token as Tokens.Strong).tokens, at),
      };
    case "em":
      return { type: "em", children: inlines((token as Tokens.Em).tokens, at) };
    case "codespan":
      return { type: "code", text: decode((token as Tokens.Codespan).text) };
    case "br":
      return { type: "break" };
    case "link": {
      const link = token as Tokens.Link;
      if (!allowedHref(link.href))
        throw new PolicyError(
          at.file,
          at.line,
          `link to "${link.href}" isn't allowed: use https:, mailto: or a path on this site (/…)`,
        );
      return {
        type: "link",
        href: link.href,
        children: inlines(link.tokens, at),
      };
    }
    case "html":
      throw new PolicyError(at.file, at.line, "raw HTML isn't allowed");
    default:
      throw new PolicyError(
        at.file,
        at.line,
        `${describe(token.type)} isn't allowed`,
      );
  }
}

// https:, mailto: or a path on this site; never //host, /\host or another
// scheme (javascript:, data:, http:).
export function allowedHref(href: string): boolean {
  if (/^https:\/\/[^\s/\\]/i.test(href)) return true;
  if (/^mailto:[^\s]+$/i.test(href)) return true;
  return /^\/(?![/\\])/.test(href) && !/[\s\\]/.test(href);
}

const names: Record<string, string> = {
  code: "a code block",
  blockquote: "a block quote",
  image: "an image",
  del: "strikethrough",
  def: "a link definition",
  checkbox: "a task list",
};

function describe(type: string): string {
  return names[type] ?? `"${type}"`;
}

const entities: Record<string, string> = {
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  apos: "'",
  nbsp: "\u00a0",
};

// The lexer leaves character references as typed; the page shows them as
// the characters they stand for (as text, never as markup).
function decode(text: string): string {
  return text.replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (whole, ref: string) => {
    if (ref[0] === "#") {
      const code =
        ref[1].toLowerCase() === "x"
          ? parseInt(ref.slice(2), 16)
          : parseInt(ref.slice(1), 10);
      return code > 0 && code <= 0x10ffff ? String.fromCodePoint(code) : whole;
    }
    return entities[ref.toLowerCase()] ?? whole;
  });
}

function plain(children: Inline[]): string {
  return children
    .map((c) =>
      "text" in c ? c.text : "children" in c ? plain(c.children) : "",
    )
    .join("");
}

// A heading's id: its text in lower case, words joined by hyphens, unique
// in the document ("cookies", "cookies-2").
function headingId(text: string, ids: Map<string, number>): string {
  const base =
    text
      .toLowerCase()
      .normalize("NFKD")
      .replace(/[\u0300-\u036f]/g, "")
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "section";
  const seen = ids.get(base) ?? 0;
  ids.set(base, seen + 1);
  return seen === 0 ? base : `${base}-${seen + 1}`;
}
