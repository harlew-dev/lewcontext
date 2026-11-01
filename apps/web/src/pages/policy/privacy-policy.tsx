import { PolicyPage } from "@/components/app/policies/policy-document";
import { NotFound } from "@/components/layout/status-page";
import { appName } from "@/lib/app";
import { readPolicy } from "@/lib/policies.server";
import type { Route } from "./+types/privacy-policy";
// /policy/privacy-policy, prerendered: the loader reads
// content/policy/privacy-policy.md at build time (lib/policies.server.ts).
// Go serves it only with POLICIES=on (plans/features/implemented/policies.md).
export function loader() {
  return readPolicy("privacy-policy");
}
export const meta: Route.MetaFunction = ({ loaderData }) =>
  loaderData
    ? [
        { title: `${loaderData.title} · ${appName}` },
        { name: "description", content: loaderData.description },
      ]
    : [{ title: appName }];
export default function PrivacyPolicy({ loaderData }: Route.ComponentProps) {
  return <PolicyPage name="privacy-policy" policy={loaderData} />;
}
// With POLICIES off Go answers this page's data file as an unknown URL, so
// arriving here (a link, or the 404 page's app loading this route) can only
// fail as "not found".
export function ErrorBoundary() {
  return <NotFound />;
}
