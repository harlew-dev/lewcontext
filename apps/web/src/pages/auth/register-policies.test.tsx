import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createRoutesStub } from "react-router";
import { describe, expect, it } from "vitest";
import { registerLoader } from "@/lib/loaders";
import { stubApi } from "@/test/api";
import { testSession } from "@/test/session";
import Register from "./register";
// Register with its real loader: the guest check and GET /api/policies.
const signedOut = { status: 401, body: { error: "Please sign in." } };
const published = {
  body: {
    enabled: true,
    termsOfService: "2026-11-01",
    privacyPolicy: "2026-10-15",
  },
};
function renderRegister() {
  const Stub = createRoutesStub([
    {
      path: "/auth/register",
      Component: Register,
      loader: ({ request }) => registerLoader(request),
    },
    { path: "/app/onboarding", Component: () => <p>onboarding</p> },
  ]);
  render(<Stub initialEntries={["/auth/register"]} />);
}
async function fillIn() {
  await screen.findByRole("heading", { name: "Create your account" });
  fireEvent.change(screen.getByLabelText("Email"), {
    target: { value: "ada@example.com" },
  });
  fireEvent.change(screen.getByLabelText("Full name"), {
    target: { value: "Ada" },
  });
  fireEvent.change(screen.getByLabelText("Password"), {
    target: { value: "correct horse battery staple" },
  });
}
const agreement = () =>
  screen.getByRole("checkbox", {
    // (jsdom's accessible names put a space before the final full stop)
    name: /^I agree to the Terms of Service and have read the Privacy Policy/,
  });
describe("agreeing to the policies at sign-up", () => {
  it.each([
    ["off", { body: { enabled: false } }],
    ["unreachable", { network: true }],
  ])("shows no checkbox when they're %s", async (_name, reply) => {
    const api = stubApi({
      "GET /api/auth/me": signedOut,
      "GET /api/policies": reply,
      "POST /api/auth/register": { status: 201, body: testSession() },
    });
    renderRegister();
    await fillIn();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(screen.queryByRole("link", { name: "Privacy Policy" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Create account" }));
    expect(await screen.findByText("onboarding")).toBeInTheDocument();
    expect(api.sent("POST")[0].body).not.toHaveProperty("policies");
  });
  it("asks for agreement, and sends the versions it showed", async () => {
    const api = stubApi({
      "GET /api/auth/me": signedOut,
      "GET /api/policies": published,
      "POST /api/auth/register": { status: 201, body: testSession() },
    });
    renderRegister();
    await fillIn();
    const box = agreement();
    expect(box).toBeRequired();
    expect(box).not.toBeChecked();
    // The documents open in a new tab, so the typed fields stay.
    for (const name of ["Terms of Service", "Privacy Policy"]) {
      const links = screen.getAllByRole("link", { name });
      expect(links[0]).toHaveAttribute("target", "_blank");
    }
    // The footer links them too.
    expect(
      screen.getAllByRole("link", { name: "Terms of Service" }),
    ).toHaveLength(2);
    fireEvent.click(box);
    expect(box).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "Create account" }));
    expect(await screen.findByText("onboarding")).toBeInTheDocument();
    expect(api.sent("POST")[0].body).toEqual({
      name: "Ada",
      email: "ada@example.com",
      password: "correct horse battery staple",
      policies: { termsOfService: "2026-11-01", privacyPolicy: "2026-10-15" },
    });
  });
  it("unticks and shows the new versions when they changed meanwhile", async () => {
    const changed =
      "Our Terms of Service or Privacy Policy changed since this page opened. Read them again, then agree.";
    const api = stubApi({
      "GET /api/auth/me": signedOut,
      "GET /api/policies": [
        published,
        {
          body: {
            enabled: true,
            termsOfService: "2026-12-01",
            privacyPolicy: "2026-10-15",
          },
        },
      ],
      "POST /api/auth/register": [
        { status: 422, body: { error: changed, field: "policies" } },
        { status: 201, body: testSession() },
      ],
    });
    renderRegister();
    await fillIn();
    fireEvent.click(agreement());
    fireEvent.click(screen.getByRole("button", { name: "Create account" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(changed);
    await waitFor(() => expect(agreement()).not.toBeChecked());
    expect(agreement()).toHaveAccessibleDescription(changed);
    fireEvent.click(agreement());
    fireEvent.click(screen.getByRole("button", { name: "Create account" }));
    expect(await screen.findByText("onboarding")).toBeInTheDocument();
    expect(api.sent("POST")[1].body).toMatchObject({
      policies: { termsOfService: "2026-12-01", privacyPolicy: "2026-10-15" },
    });
  });
});
