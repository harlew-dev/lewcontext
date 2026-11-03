import { expect, test } from "@playwright/test";
import { signUpThroughApi } from "./mail";

test("an owner reads the workspace's changes, filters them and sees no addresses", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const session: { csrfToken: string } = await signUpThroughApi(page.request, {
    name: "Log reader",
    email: `e2e-logs-${Date.now()}@lewcontext.test`,
    password: "An unattended integration test password!",
  });
  const headers = {
    Origin: process.env.APP_ORIGIN!,
    "X-CSRF-Token": session.csrfToken,
  };
  const created = await page.request.post("/api/workspaces", {
    headers,
    data: { name: "Log workspace" },
  });
  expect(created.status()).toBe(201);
  const workspace: { id: string } = await created.json();
  const project = await page.request.post(
    `/api/workspaces/${workspace.id}/projects`,
    {
      headers,
      data: {
        name: "Website",
        description: "",
        status: "Planned",
        color: "blue",
      },
    },
  );
  expect(project.status()).toBe(201);
  const { id } = (await project.json()) as { id: string };
  const update = await page.request.patch(
    `/api/workspaces/${workspace.id}/projects/${id}`,
    {
      headers,
      data: {
        name: "Marketing site",
        description: "",
        status: "In progress",
        color: "blue",
      },
    },
  );
  expect(update.status()).toBe(200);

  await page.goto(`/app/${workspace.id}/settings/events`);
  await expect(
    page.getByRole("link", { name: "Logs", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await expect(page.getByText("updated a project")).toBeVisible();
  await expect(page.getByText(/created the project “Website”/)).toBeVisible();
  await expect(
    page.getByText(
      "name: Website → Marketing site · status: Planned → In progress",
    ),
  ).toBeVisible();

  // Details open in place, as labelled values.
  await page
    .getByRole("button", { name: /Details: Log reader updated a project/ })
    .click();
  await expect(page.getByText("Name changed")).toBeVisible();
  await expect(
    page.getByText("Website → Marketing site", { exact: true }),
  ).toBeVisible();

  // A search narrows the list, and lives in the URL.
  await page
    .getByRole("searchbox", { name: "Search changes" })
    .fill("marketing");
  await expect(page).toHaveURL(/q=marketing/);
  await expect(page.getByText(/created the project/)).toHaveCount(0);
  await expect(page.getByText("updated a project")).toBeVisible();

  // The ⋯ menu opens the change's own page, with the request ID to quote.
  await page.getByRole("searchbox", { name: "Search changes" }).fill("");
  await expect(page).not.toHaveURL(/q=marketing/);
  await page
    .getByRole("button", { name: /Actions for Log reader updated a project/ })
    .click();
  await page.getByRole("menuitem", { name: "View event" }).click();
  await expect(page).toHaveURL(/\/settings\/events\/evt_/);
  await expect(
    page.getByRole("heading", { name: "Log reader updated a project" }),
  ).toBeVisible();
  await expect(page.getByText(/^req_[0-9A-Za-z]{14}$/).first()).toBeVisible();
  await expect(page.getByText("Other changes in this request")).toBeVisible();

  // The person who made it opens their page, with its tabs.
  await page.getByRole("link", { name: "Log reader" }).first().click();
  await expect(page).toHaveURL(/\/settings\/members\/usr_/);
  await expect(page.getByRole("heading", { name: "Log reader" })).toBeVisible();
  await page.getByRole("link", { name: "Logs", exact: true }).last().click();
  await expect(page).toHaveURL(/tab=logs/);
  await expect(page.getByText("created the project “Website”")).toBeVisible();

  // The search finds an event by what it is, not only by its text.
  await page.goto(`/app/${workspace.id}/settings/events`);
  await page.getByRole("searchbox", { name: "Search changes" }).fill("project");
  await expect(page.getByText("updated a project")).toBeVisible();
  await page.getByRole("searchbox", { name: "Search changes" }).fill("api key");
  await expect(page.getByText("No changes match these filters")).toBeVisible();

  // The API sends the request ID, never an address or a browser.
  const api = await page.request.get(`/api/workspaces/${workspace.id}/events`);
  expect(api.status()).toBe(200);
  const body = await api.text();
  expect(body).toMatch(/"requestId":"req_/);
  expect(body).not.toMatch(/"ip"|userAgent|request_id/);
  expect(errors).toEqual([]);
});

test("an API key's page shows its overview and its own log", async ({
  page,
}) => {
  const password = "An unattended integration test password!";
  const session: { csrfToken: string } = await signUpThroughApi(page.request, {
    name: "Key reader",
    email: `e2e-key-page-${Date.now()}@lewcontext.test`,
    password,
  });
  const headers = {
    Origin: process.env.APP_ORIGIN!,
    "X-CSRF-Token": session.csrfToken,
  };
  const created = await page.request.post("/api/workspaces", {
    headers,
    data: { name: "Key page workspace" },
  });
  const workspace: { id: string } = await created.json();
  const made = await page.request.post(
    `/api/workspaces/${workspace.id}/api-keys`,
    {
      headers,
      data: {
        name: "Deploy bot",
        projectAccess: "all",
        projects: [],
        scopes: ["project.index", "project.show"],
        expiresAt: null,
        password,
      },
    },
  );
  expect(made.status()).toBe(201);
  const { key } = (await made.json()) as { key: { id: string } };

  // The table: a name, a green status and one grouped badge in words.
  await page.goto(`/app/${workspace.id}/settings/api-keys`);
  const row = page.getByRole("row", { name: /Deploy bot/ });
  await expect(row.getByText("Active")).toBeVisible();
  await expect(row.getByText("All projects: List, View")).toBeVisible();
  await row.getByRole("link", { name: "Deploy bot" }).click();

  await expect(page).toHaveURL(new RegExp(`/settings/api-keys/${key.id}$`));
  await expect(page.getByRole("heading", { name: "Deploy bot" })).toBeVisible();
  await expect(page.getByText("List, View", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Logs", exact: true }).last().click();
  await expect(page.getByText(`created the API key ${key.id}`)).toBeVisible();
});
