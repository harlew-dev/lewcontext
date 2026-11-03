import { expect, test } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { signUpThroughApi } from "./mail";
import { codeFor } from "./totp";

test("workspace and personal keys are scoped, hidden until revealed, and revocable", async ({
  page,
}, testInfo) => {
  const screenshot = async (name: string) => {
    const path = testInfo.outputPath(`${name}.png`);
    await page.screenshot({ path, animations: "disabled" });
    await testInfo.attach(name, { path, contentType: "image/png" });
  };
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (
      message.type() === "error" &&
      message.text().includes("Content Security Policy")
    )
      errors.push(message.text());
  });
  const password = "An unattended integration test password!";
  const session: { csrfToken: string } = await signUpThroughApi(page.request, {
    name: "API integration tester",
    email: `e2e-api-keys-${Date.now()}@lewcontext.test`,
    password,
  });
  const headers = {
    Origin: process.env.APP_ORIGIN!,
    "X-CSRF-Token": session.csrfToken,
  };
  const createdWorkspace = await page.request.post("/api/workspaces", {
    headers,
    data: { name: "Key integration workspace" },
  });
  expect(createdWorkspace.status()).toBe(201);
  const workspace: { id: string } = await createdWorkspace.json();
  try {
    const makeProject = async (name: string, description = "") => {
      const response = await page.request.post(
        `/api/workspaces/${workspace.id}/projects`,
        {
          headers,
          data: { name, description, status: "Planned", color: "blue" },
        },
      );
      expect(response.status()).toBe(201);
      return (await response.json()) as { id: string };
    };
    const target = await makeProject(
      "Selected target",
      "Searchable integration project",
    );
    const other = await makeProject("Unselected target");
    for (let index = 1; index <= 21; index++)
      await makeProject(`Recent project ${index}`);
    await page.goto(`/app/${workspace.id}/settings/api-keys`);
    await expect(
      page.getByRole("link", { name: "API Keys", exact: true }),
    ).toHaveAttribute("aria-current", "page");
    await page.getByRole("button", { name: "Add API Key" }).click();
    await page.getByLabel("Name", { exact: true }).fill("Scoped integration");
    await page.getByRole("combobox", { name: /Expiry/ }).click();
    await page.getByRole("option", { name: "Never", exact: true }).click();
    await page.getByRole("combobox", { name: /Project access/ }).click();
    await page
      .getByRole("option", { name: "Selected projects", exact: true })
      .click();
    const projects = page.getByRole("combobox", {
      name: "Projects",
      exact: true,
    });
    await projects.click();
    await expect(
      page.getByRole("option", { name: /^Recent project 21/ }),
    ).toBeVisible();
    await expect(
      page.getByRole("option", { name: /^Selected target/ }),
    ).toHaveCount(0);
    const nextPage = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname.endsWith("/projects") &&
        new URL(response.url()).searchParams.get("page") === "2",
    );
    await page
      .getByRole("listbox")
      .locator("..")
      .evaluate((element) => {
        element.scrollTop = element.scrollHeight;
      });
    expect((await nextPage).status()).toBe(200);
    const searched = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname.endsWith("/projects") &&
        new URL(response.url()).searchParams.get("q") ===
          "Searchable integration",
    );
    await projects.fill("Searchable integration");
    expect((await searched).status()).toBe(200);
    await page.getByRole("option", { name: /^Selected target/ }).click();
    await page.keyboard.press("Escape");
    await page
      .getByRole("combobox", { name: "Permissions", exact: true })
      .fill("Update");
    await page.getByRole("option", { name: /^Update/ }).click();
    await page.keyboard.press("Escape");
    await page.getByLabel("Current password").fill(password);
    await screenshot("add-key-dialog");
    const response = page.waitForResponse(
      (r) => r.request().method() === "POST" && r.url().endsWith("/api-keys"),
    );
    await page
      .getByRole("button", { name: "Add API Key", exact: true })
      .click();
    const issuedResponse = await response;
    expect(issuedResponse.status()).toBe(201);
    const issued: { key: { id: string }; secret: string } =
      await issuedResponse.json();
    await expect(
      page.getByRole("button", { name: "Reveal API key" }),
    ).toBeVisible();
    await expect(page.locator("body")).not.toContainText(issued.secret);
    await screenshot("one-time-key-masked");
    await page.getByRole("button", { name: "Done", exact: true }).click();
    const bearer = { Authorization: `Bearer ${issued.secret}` };
    const base = `/api/v1/workspaces/${workspace.id}/projects`;
    const listed = await page.request.get(base, { headers: bearer });
    expect(listed.status()).toBe(200);
    const list: { data: { id: string }[]; pages: { items: number } } =
      await listed.json();
    expect(list.pages.items).toBe(1);
    expect(list.data.map((p) => p.id)).toEqual([target.id]);
    expect(
      (
        await page.request.get(`${base}/${other.id}`, { headers: bearer })
      ).status(),
    ).toBe(404);
    expect(
      (
        await page.request.patch(`${base}/${target.id}`, {
          headers: bearer,
          data: {
            name: "Updated by integration",
            description: "",
            status: "Completed",
            color: "blue",
          },
        })
      ).status(),
    ).toBe(200);
    await page
      .getByRole("button", { name: "Actions for Scoped integration" })
      .click();
    await page.getByRole("menuitem", { name: "Revoke", exact: true }).click();
    await screenshot("revoke-confirmation");
    await page
      .getByRole("button", { name: "Revoke API key", exact: true })
      .click();
    await expect(
      page.getByRole("dialog", { name: "Revoke Scoped integration?" }),
    ).not.toBeVisible();
    expect((await page.request.get(base, { headers: bearer })).status()).toBe(
      401,
    );

    await page.goto("/app/settings/api-keys");
    await page.getByRole("button", { name: "Add API Key" }).click();
    await page.getByLabel("Name", { exact: true }).fill("My personal script");
    await page.getByLabel("Current password").fill(password);
    const personalResponse = page.waitForResponse(
      (r) =>
        r.request().method() === "POST" &&
        r.url().endsWith("/api/auth/api-keys"),
    );
    await page
      .getByRole("button", { name: "Add API Key", exact: true })
      .click();
    const personal: { secret: string } = await (await personalResponse).json();
    await page.getByRole("button", { name: "Done", exact: true }).click();
    expect(
      (
        await page.request.get(base, {
          headers: { Authorization: `Bearer ${personal.secret}` },
        })
      ).status(),
    ).toBe(200);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload();
    await expect(
      page.getByText("My personal script", { exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBeLessThanOrEqual(390);
    await screenshot("personal-key-mobile");
    expect(errors).toEqual([]);
  } finally {
    await page.request.delete(`/api/workspaces/${workspace.id}`, {
      headers,
      data: { name: "Key integration workspace", password },
    });
    await page.request.delete("/api/auth/me", { headers, data: { password } });
  }
});

test("personal keys follow live roles and stay revoked after rejoining", async ({
  page,
  browser,
}) => {
  const password = "A separate personal automation password!";
  const memberContext = await browser.newContext({
    baseURL: process.env.APP_ORIGIN,
  });
  const owner = await signUpThroughApi(page.request, {
    name: "Key workspace owner",
    email: `e2e-key-owner-${Date.now()}@lewcontext.test`,
    password,
  });
  const member = await signUpThroughApi(memberContext.request, {
    name: "Key member",
    email: `e2e-key-member-${Date.now()}@lewcontext.test`,
    password,
  });
  const headers = {
    Origin: process.env.APP_ORIGIN!,
    "X-CSRF-Token": owner.csrfToken,
  };
  const memberHeaders = { ...headers, "X-CSRF-Token": member.csrfToken };
  const response = await page.request.post("/api/workspaces", {
    headers,
    data: { name: "Live personal authority" },
  });
  expect(response.status()).toBe(201);
  const workspace: { id: string } = await response.json();
  const join = () =>
    execFileSync(
      "psql",
      [
        process.env.DATABASE_URL!,
        "-v",
        "ON_ERROR_STOP=1",
        "-v",
        `workspace=${workspace.id}`,
        "-v",
        `member=${member.user.id}`,
      ],
      {
        input:
          "INSERT INTO workspace_user (user_id, workspace_id, role) SELECT u.id, w.id, 'member' FROM users u, workspaces w WHERE u.public_id = :'member' AND w.public_id = :'workspace';",
        stdio: ["pipe", "pipe", "pipe"],
      },
    );
  try {
    join();
    const created = await page.request.post(
      `/api/workspaces/${workspace.id}/projects`,
      {
        headers,
        data: {
          name: "Role target",
          description: "",
          status: "Planned",
          color: "blue",
        },
      },
    );
    expect(created.status()).toBe(201);
    const project: { id: string } = await created.json();
    const issued = await memberContext.request.post("/api/auth/api-keys", {
      headers: memberHeaders,
      data: {
        workspace: workspace.id,
        name: "Member script",
        projectAccess: "all",
        projects: [],
        scopes: ["project.index", "project.show", "project.update"],
        expiresAt: null,
        password,
      },
    });
    expect(issued.status()).toBe(201);
    const { secret }: { secret: string } = await issued.json();
    const bearer = { Authorization: `Bearer ${secret}` };
    const base = `/api/v1/workspaces/${workspace.id}/projects`;
    const update = () =>
      page.request.patch(`${base}/${project.id}`, {
        headers: bearer,
        data: {
          name: "Script update",
          description: "",
          status: "Completed",
          color: "blue",
        },
      });
    expect((await update()).status()).toBe(200);
    const membership = `/api/workspaces/${workspace.id}/members/${member.user.id}`;
    expect(
      (
        await page.request.patch(membership, {
          headers,
          data: { role: "viewer" },
        })
      ).status(),
    ).toBe(200);
    expect((await update()).status()).toBe(403);
    expect((await page.request.get(base, { headers: bearer })).status()).toBe(
      200,
    );
    expect(
      (
        await page.request.patch(membership, {
          headers,
          data: { role: "member" },
        })
      ).status(),
    ).toBe(200);
    expect((await update()).status()).toBe(200);
    expect((await page.request.delete(membership, { headers })).status()).toBe(
      204,
    );
    expect((await page.request.get(base, { headers: bearer })).status()).toBe(
      401,
    );
    join();
    expect((await page.request.get(base, { headers: bearer })).status()).toBe(
      401,
    );
  } finally {
    await page.request.delete(`/api/workspaces/${workspace.id}`, {
      headers,
      data: { name: "Live personal authority", password },
    });
    await page.request.delete("/api/auth/me", { headers, data: { password } });
    await memberContext.request.delete("/api/auth/me", {
      headers: memberHeaders,
      data: { password },
    });
    await memberContext.close();
  }
});

test("two-factor proof opens over the filled key dialog and supports recovery codes", async ({
  page,
}, testInfo) => {
  const password = "A nested authentication test password!";
  const session = await signUpThroughApi(page.request, {
    name: "Protected key owner",
    email: `e2e-nested-key-${Date.now()}@lewcontext.test`,
    password,
  });
  const headers = {
    Origin: process.env.APP_ORIGIN!,
    "X-CSRF-Token": session.csrfToken,
  };
  const created = await page.request.post("/api/workspaces", {
    headers,
    data: { name: "Protected key workspace" },
  });
  expect(created.status()).toBe(201);
  const workspace: { id: string } = await created.json();
  try {
    const setup = await page.request.post("/api/auth/mfa/setup", {
      headers,
      data: { password },
    });
    expect(setup.status()).toBe(200);
    const { secret }: { secret: string } = await setup.json();
    const usedCode = codeFor(secret, 0);
    const enabled = await page.request.post("/api/auth/mfa", {
      headers,
      data: { code: usedCode },
    });
    expect(enabled.status()).toBe(201);
    const { recoveryCodes }: { recoveryCodes: string[] } = await enabled.json();
    await page.goto(`/app/${workspace.id}/settings/api-keys`);
    await page
      .getByRole("button", { name: "Add API Key", exact: true })
      .click();
    await page
      .getByLabel("Name", { exact: true })
      .fill("Protected integration");
    await expect(page.getByLabel("Authentication code")).toHaveCount(0);
    const form = page.getByRole("dialog", { name: "Add API Key", exact: true });
    await form
      .getByRole("button", { name: "Add API Key", exact: true })
      .click();
    const authentication = page.getByRole("dialog", {
      name: "Confirm your identity",
    });
    await expect(
      authentication.getByLabel("Authentication code"),
    ).toBeFocused();
    await expect(
      page.locator('[role="dialog"][data-nested-dialog-open]'),
    ).toHaveCount(1);
    const screenshot = testInfo.outputPath("nested-key-authentication.png");
    await page.screenshot({ path: screenshot, animations: "disabled" });
    await testInfo.attach("nested-key-authentication", {
      path: screenshot,
      contentType: "image/png",
    });
    await authentication
      .getByRole("button", { name: "Cancel", exact: true })
      .click();
    await expect(authentication).not.toBeVisible();
    await expect(form.getByLabel("Name", { exact: true })).toHaveValue(
      "Protected integration",
    );
    await expect(
      form.getByRole("button", { name: "Add API Key", exact: true }),
    ).toBeFocused();
    await form
      .getByRole("button", { name: "Add API Key", exact: true })
      .click();
    await authentication.getByLabel("Authentication code").fill(usedCode);
    const refused = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.url().endsWith("/api-keys"),
    );
    await authentication
      .getByRole("button", { name: "Add API Key", exact: true })
      .click();
    expect((await refused).status()).toBe(422);
    await expect(authentication.getByLabel("Authentication code")).toHaveValue(
      "",
    );
    await authentication
      .getByRole("button", { name: /Use a recovery code/ })
      .click();
    await authentication.getByLabel("Recovery code").fill(recoveryCodes[0]);
    const issued = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.url().endsWith("/api-keys"),
    );
    await authentication
      .getByRole("button", { name: "Add API Key", exact: true })
      .click();
    expect((await issued).status()).toBe(201);
    await expect(authentication).not.toBeVisible();
    await expect(
      page.getByRole("button", { name: "Reveal API key" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Done", exact: true }).click();
  } finally {
    await page.request.delete(`/api/workspaces/${workspace.id}`, {
      headers,
      data: { name: "Protected key workspace", password },
    });
    await page.request.delete("/api/auth/me", { headers, data: { password } });
  }
});
