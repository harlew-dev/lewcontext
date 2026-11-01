#!/usr/bin/env node
// Checks the relative links in the repository's markdown, the files git
// tracks or has staged (docs/checks.md): every linked file exists, and every
// #anchor matches a heading in its target (GitHub's heading ids). A tracked file linking to an untracked one
// is a warning: stage the new file, or the link is dead in the repository.
// It only reads files and runs `git ls-files`: no network, no eval.
//
//   node scripts/check-doc-links.mjs          check the repository
//   node scripts/check-doc-links.mjs --test   run the script's own checks
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

// Links allowed to be dead, each with its reason: `file → target` (the link
// as written). A new one needs the user's agreement (docs/checks.md).
export const allowedDeadLinks = {
  "plans/README.md → future/framework.md":
    "existing; to fix: a plan that is untracked in the main checkout (plans/future/), not in the repository",
};

// GitHub's id for a heading: lowercase, punctuation other than - and _
// dropped, spaces as -. Repeats get -1, -2, … (headingIds).
export function slug(heading) {
  return heading
    .trim()
    .toLowerCase()
    .replace(/<[^>]*>/g, "")
    .replace(/[^\p{L}\p{N}\s_-]/gu, "")
    .replace(/\s/g, "-");
}

// Lines outside fenced code blocks, with their 1-based numbers.
function proseLines(text) {
  const lines = [];
  let fence = null;
  text.split("\n").forEach((line, i) => {
    const marker = line.match(/^\s*(`{3,}|~{3,})/);
    if (marker) {
      if (!fence) fence = marker[1][0];
      else if (marker[1][0] === fence) fence = null;
      return;
    }
    if (!fence) lines.push({ line, number: i + 1 });
  });
  return lines;
}

// Every heading's id in a markdown text, repeats numbered as GitHub does.
export function headingIds(text) {
  const ids = new Set();
  const seen = new Map();
  for (const { line } of proseLines(text)) {
    const heading = line.match(/^#{1,6}\s+(.*?)\s*#*\s*$/);
    if (!heading) continue;
    const base = slug(heading[1].replace(/`/g, ""));
    const count = seen.get(base) ?? 0;
    seen.set(base, count + 1);
    ids.add(count === 0 ? base : `${base}-${count}`);
  }
  return ids;
}

// The relative links in a markdown text: [text](target), outside code.
export function relativeLinks(text) {
  const links = [];
  for (const { line, number } of proseLines(text)) {
    const prose = line.replace(/`[^`]*`/g, "");
    for (const match of prose.matchAll(/\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g)) {
      const target = match[1].replace(/^<|>$/g, "");
      if (/^[a-z][a-z0-9+.-]*:/i.test(target)) continue; // https:, mailto:
      links.push({ target, line: number });
    }
  }
  return links;
}

// The problems with one file's links. read(path) returns a file's text or
// null; tracked(path) whether git tracks it.
export function checkFile(file, text, { read, exists, tracked }) {
  const errors = [];
  const warnings = [];
  for (const { target, line } of relativeLinks(text)) {
    const [path, anchor] = target.split("#");
    const linked = path ? join(dirname(file), decodeURI(path)) : file;
    const where = `${file}:${line}: ${target}`;
    if (path && !exists(linked)) {
      if (!allowedDeadLinks[`${file} → ${target}`])
        errors.push(`${where} → no such file`);
      continue;
    }
    if (anchor !== undefined && linked.endsWith(".md")) {
      const body = read(linked);
      if (body !== null && !headingIds(body).has(decodeURIComponent(anchor)))
        errors.push(`${where} → no heading #${anchor} in ${linked}`);
    }
    if (path && tracked(file) && !tracked(linked))
      warnings.push(
        `${where} → ${linked} isn't tracked: stage it, or the link is dead in the repository`,
      );
  }
  return { errors, warnings };
}

function git(...args) {
  return execFileSync("git", args, { cwd: root, encoding: "utf8" })
    .split("\n")
    .filter(Boolean);
}

function checkRepository() {
  const skip = /(^|\/)(node_modules|build|dist|test-results|playwright-report)\//;
  const trackedFiles = new Set(git("ls-files"));
  // Only files git tracks or has staged: an untracked file may be someone
  // else's work in progress in a shared checkout. Stage a new doc to check it.
  const markdown = git("ls-files", "*.md").filter((file) => !skip.test(file));
  // Directories count as tracked when git tracks a file inside them.
  const tracked = (path) =>
    trackedFiles.has(path) ||
    [...trackedFiles].some((f) => f.startsWith(`${path.replace(/\/$/, "")}/`));
  const cache = new Map();
  const read = (path) => {
    if (!cache.has(path)) {
      const full = join(root, path);
      cache.set(
        path,
        existsSync(full) && statSync(full).isFile()
          ? readFileSync(full, "utf8")
          : null,
      );
    }
    return cache.get(path);
  };
  const exists = (path) => existsSync(join(root, path));
  let errors = 0;
  let warnings = 0;
  for (const file of markdown) {
    const result = checkFile(file, read(file), { read, exists, tracked });
    for (const e of result.errors) console.error(e);
    for (const w of result.warnings) console.warn(`warning: ${w}`);
    errors += result.errors.length;
    warnings += result.warnings.length;
  }
  console.log(
    `Doc links: ${markdown.length} files, ${errors} broken, ${warnings} warnings.`,
  );
  if (errors > 0) {
    console.error(
      "Fix the link or the anchor rather than ignoring it (docs/checks.md).",
    );
    process.exit(1);
  }
}

// The script's own checks: a dead link, a dead anchor and a link to an
// untracked file are found; good links, code and URLs aren't.
function selfTest() {
  const files = {
    "docs/a.md": [
      "# Title",
      "## Store methods (`internal/store`)",
      "## Repeat",
      "## Repeat",
      "See [b](b.md), [b's part](b.md#part-one), [here](#store-methods-internalstore) and [again](#repeat-1).",
      "Dead: [x](missing.md), [y](b.md#nope), [z](#nowhere).",
      "Untracked: [n](new.md). Web: [w](https://example.com/x.md). Code: `[c](missing.md)`.",
      "```",
      "[fenced](missing.md)",
      "```",
    ].join("\n"),
    "docs/b.md": "# B\n\n## Part one\n",
    "docs/new.md": "# New\n",
  };
  const read = (path) => files[path] ?? null;
  const { errors, warnings } = checkFile("docs/a.md", files["docs/a.md"], {
    read,
    exists: (path) => path in files,
    tracked: (path) => path !== "docs/new.md",
  });
  const expect = (cond, message) => {
    if (!cond) {
      console.error(`check-doc-links self-test: ${message}`);
      process.exit(1);
    }
  };
  expect(errors.length === 3, `errors: ${JSON.stringify(errors)}`);
  expect(errors[0].includes("missing.md → no such file"), errors[0]);
  expect(errors[1].includes("no heading #nope"), errors[1]);
  expect(errors[2].includes("no heading #nowhere"), errors[2]);
  expect(
    warnings.length === 1 && warnings[0].includes("new.md isn't tracked"),
    `warnings: ${JSON.stringify(warnings)}`,
  );
  expect(slug("Store methods (`internal/store`)".replace(/`/g, "")) === "store-methods-internalstore", "slug");
  expect(slug("Who may add an exception?") === "who-may-add-an-exception", "slug punctuation");
  console.log("check-doc-links self-test: ok");
}

if (process.argv.includes("--test")) selfTest();
else checkRepository();
