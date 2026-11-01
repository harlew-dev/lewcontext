import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vitest";
import type { Policy } from "@/types/policies";
import { PolicyPage } from "./policy-document";
const policy: Policy = {
  title: "Terms of Service",
  description: "",
  updated: "2026-11-01",
  boilerplate: false,
  blocks: [
    {
      type: "heading",
      level: 2,
      id: "cookies",
      children: [{ type: "text", text: "Cookies" }],
    },
    {
      type: "paragraph",
      children: [
        { type: "text", text: "<script>alert(1)</script> & " },
        {
          type: "link",
          href: "mailto:a@example.com",
          children: [{ type: "text", text: "write to us" }],
        },
      ],
    },
    {
      type: "table",
      header: [[{ type: "text", text: "Data" }]],
      rows: [
        [[{ type: "strong", children: [{ type: "text", text: "Email" }] }]],
      ],
    },
  ],
};
function renderPage(p: Policy) {
  render(
    <MemoryRouter>
      <PolicyPage name="terms-of-service" policy={p} />
    </MemoryRouter>,
  );
}
describe("PolicyPage", () => {
  it("shows the document as text, never as markup", () => {
    renderPage(policy);
    expect(
      screen.getByRole("heading", { level: 1, name: "Terms of Service" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Last updated November 1, 2026"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { level: 2, name: "Cookies" }),
    ).toHaveAttribute("id", "cookies");
    expect(
      screen.getByText(/<script>alert\(1\)<\/script> &/),
    ).toBeInTheDocument();
    expect(document.querySelector("article script")).toBeNull();
    expect(screen.getByRole("link", { name: "write to us" })).toHaveAttribute(
      "href",
      "mailto:a@example.com",
    );
    expect(screen.getByRole("cell", { name: "Email" })).toBeInTheDocument();
    // Both documents are linked below, and no template warning.
    expect(
      screen.getByRole("link", { name: "Privacy Policy" }),
    ).toHaveAttribute("href", "/policy/privacy-policy");
    expect(screen.queryByText(/Template text/)).toBeNull();
  });
  it("warns while the document is still the template", () => {
    renderPage({ ...policy, boilerplate: true });
    expect(screen.getByText(/Template text/)).toHaveTextContent(
      "Template text: edit apps/web/content/policy/terms-of-service.md and remove its boilerplate line before publishing.",
    );
  });
});
