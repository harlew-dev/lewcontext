import { expect, it } from "vitest";
import { stubApi } from "@/test/api";
import { client, paths, type ApiPath, type PendingEmailChange } from ".";
import { browserHttp } from "./http";

it("keeps the singleton's account proof and inbox code in separate bodies", async () => {
  const stub = stubApi({ "*": { body: {} } });
  const change = client(browserHttp("csrf")).auth().emailChange();
  const typed: ApiPath<{ pending: PendingEmailChange | null }> = paths.auth
    .emailChange()
    .show();
  expect(typed).toBe("/api/auth/email-change");
  expect(Object.keys(change)).toEqual(["show", "store", "update", "destroy"]);
  expect(Object.keys(paths.auth.emailChange())).toEqual([
    "show",
    "store",
    "update",
    "destroy",
    "matches",
  ]);
  await change.show();
  await change.store({
    email: "next@example.test",
    currentPassword: "password",
  });
  await change.store({ email: "next@example.test", code: "recovery-code" });
  await change.update("123456");
  await change.destroy();
  expect(
    stub.sent().map(({ method, url, body }) => ({ method, url, body })),
  ).toEqual([
    { method: "GET", url: typed, body: undefined },
    {
      method: "POST",
      url: typed,
      body: { email: "next@example.test", currentPassword: "password" },
    },
    {
      method: "POST",
      url: typed,
      body: { email: "next@example.test", code: "recovery-code" },
    },
    { method: "PATCH", url: typed, body: { code: "123456" } },
    { method: "DELETE", url: typed, body: undefined },
  ]);
  for (const sent of stub.sent().filter((sent) => sent.method !== "GET"))
    expect(sent.csrf).toBe("csrf");
});
