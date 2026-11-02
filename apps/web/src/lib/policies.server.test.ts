import { describe, expect, it } from "vitest";
import { policyNames } from "@/types/policies";
import { allowedHref, parsePolicy, readPolicy } from "./policies.server";
const front =
  "---\ntitle: Terms\ndescription: The terms.\nupdated: 2026-11-01\n---\n";
function parse(body: string, meta = front) {
  return parsePolicy(meta + body, "terms.md");
}
describe("parsePolicy", () => {
  it("reads the front matter", () => {
    const p = parse(
      "Hello.",
      front.replace("---\n", "---\nboilerplate: true\n"),
    );
    expect(p).toMatchObject({
      title: "Terms",
      description: "The terms.",
      updated: "2026-11-01",
      boilerplate: true,
    });
    expect(parse("Hello.").boilerplate).toBe(false);
  });
  it.each([
    ["no front matter", "Hello.", "", /terms\.md:1: start with a front matter/],
    ["no title", "x", "---\nupdated: 2026-11-01\n---\n", /needs a title/],
    ["no date", "x", "---\ntitle: T\n---\n", /updated: a real YYYY-MM-DD/],
    [
      "not a real date",
      "x",
      "---\ntitle: T\nupdated: 2026-02-30\n---\n",
      /real YYYY-MM-DD/,
    ],
    [
      "unknown key",
      "x",
      "---\ntitle: T\nupdated: 2026-11-01\nauthor: me\n---\n",
      /terms\.md:4: unknown front matter "author: me"/,
    ],
    [
      "boilerplate not a flag",
      "x",
      "---\ntitle: T\nupdated: 2026-11-01\nboilerplate: yes\n---\n",
      /true or false/,
    ],
  ])("refuses %s", (_name, body, meta, error) => {
    expect(() => parsePolicy(meta + body, "terms.md")).toThrow(error);
  });
  it("turns each allowed piece of Markdown into a block", () => {
    const p = parse(
      [
        "## 1. Who we are",
        "",
        "We are **[Company]**, *really*, at `HQ` &amp; [here](https://example.com) or [mail](mailto:a@example.com) or [privacy](/policy/privacy-policy#cookies).",
        "",
        "### Who we are",
        "",
        "- one",
        "  - nested *two*",
        "- three",
        "",
        "1. first",
        "",
        "| Data | Why |",
        "| --- | --- |",
        "| Email | **Sign in** |",
        "",
        "---",
        "",
        "#### Who we are",
      ].join("\n"),
    );
    expect(p.blocks).toEqual([
      {
        type: "heading",
        level: 2,
        id: "1-who-we-are",
        children: [{ type: "text", text: "1. Who we are" }],
      },
      {
        type: "paragraph",
        children: [
          { type: "text", text: "We are " },
          { type: "strong", children: [{ type: "text", text: "[Company]" }] },
          { type: "text", text: ", " },
          { type: "em", children: [{ type: "text", text: "really" }] },
          { type: "text", text: ", at " },
          { type: "code", text: "HQ" },
          { type: "text", text: " & " },
          {
            type: "link",
            href: "https://example.com",
            children: [{ type: "text", text: "here" }],
          },
          { type: "text", text: " or " },
          {
            type: "link",
            href: "mailto:a@example.com",
            children: [{ type: "text", text: "mail" }],
          },
          { type: "text", text: " or " },
          {
            type: "link",
            href: "/policy/privacy-policy#cookies",
            children: [{ type: "text", text: "privacy" }],
          },
          { type: "text", text: "." },
        ],
      },
      {
        type: "heading",
        level: 3,
        id: "who-we-are",
        children: [{ type: "text", text: "Who we are" }],
      },
      {
        type: "list",
        ordered: false,
        items: [
          [
            { type: "paragraph", children: [{ type: "text", text: "one" }] },
            {
              type: "list",
              ordered: false,
              items: [
                [
                  {
                    type: "paragraph",
                    children: [
                      { type: "text", text: "nested " },
                      { type: "em", children: [{ type: "text", text: "two" }] },
                    ],
                  },
                ],
              ],
            },
          ],
          [{ type: "paragraph", children: [{ type: "text", text: "three" }] }],
        ],
      },
      {
        type: "list",
        ordered: true,
        items: [
          [{ type: "paragraph", children: [{ type: "text", text: "first" }] }],
        ],
      },
      {
        type: "table",
        header: [
          [{ type: "text", text: "Data" }],
          [{ type: "text", text: "Why" }],
        ],
        rows: [
          [
            [{ type: "text", text: "Email" }],
            [{ type: "strong", children: [{ type: "text", text: "Sign in" }] }],
          ],
        ],
      },
      { type: "rule" },
      // A repeated heading gets its own id.
      {
        type: "heading",
        level: 4,
        id: "who-we-are-2",
        children: [{ type: "text", text: "Who we are" }],
      },
    ]);
  });
  it.each([
    [
      "raw HTML",
      "Hello\n\n<script>alert(1)</script>",
      /terms\.md:8: raw HTML isn't allowed/,
    ],
    ["inline HTML", "Hello <b>there</b>", /raw HTML isn't allowed/],
    ["an image", "![x](https://example.com/x.png)", /an image isn't allowed/],
    ["a code block", "```\ncode\n```", /a code block isn't allowed/],
    ["a block quote", "> quoted", /a block quote isn't allowed/],
    ["a # heading", "# Title again", /use headings ## to ####/],
    ["a ##### heading", "##### Deep", /use headings ## to ####/],
    [
      "a javascript: link",
      "[x](javascript:alert(1))",
      /link to "javascript:alert\(1\)" isn't allowed/,
    ],
    ["an http: link", "[x](http://example.com)", /isn't allowed/],
    ["a protocol-relative link", "[x](//evil.example)", /isn't allowed/],
    ["a data: link", "[x](data:text/html,hi)", /isn't allowed/],
    ["strikethrough", "~~gone~~", /strikethrough isn't allowed/],
    [
      "a reference-style link definition",
      "See [terms][t].\n\n[t]: https://example.com",
      /link definition isn't allowed/,
    ],
  ])("refuses %s, saying where", (_name, body, error) => {
    expect(() => parse(body)).toThrow(error);
  });
});
describe("allowedHref", () => {
  it.each([
    ["https://example.com/terms", true],
    ["mailto:privacy@example.com", true],
    ["/policy/privacy-policy", true],
    ["/policy/privacy-policy#cookies", true],
    ["//evil.example", false],
    ["/\\evil.example", false],
    ["javascript:alert(1)", false],
    ["JAVASCRIPT:alert(1)", false],
    ["http://example.com", false],
    ["https://", false],
    ["#cookies", false],
    ["relative/path", false],
  ])("%s → %s", (href, ok) => {
    expect(allowedHref(href)).toBe(ok);
  });
});
describe("the committed documents", () => {
  it.each(policyNames)("%s reads, as the build will", (name) => {
    const p = readPolicy(name);
    expect(p.title).not.toBe("");
    expect(p.updated).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(p.blocks.length).toBeGreaterThan(3);
  });
});
