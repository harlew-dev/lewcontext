import { PolicyPage } from "@/components/app/policies/policy-document";
import { appName } from "@/lib/app";
import { readPolicy } from "@/lib/policies.server";
import type { Route } from "./+types/terms-of-service";
// /policy/terms-of-service, prerendered: the loader reads
// content/policy/terms-of-service.md at build time (lib/policies.server.ts).
// Go serves it only with POLICIES=on (plans/features/implemented/policies.md).
export function loader() {
  return readPolicy("terms-of-service");
}
export const meta: Route.MetaFunction = ({ loaderData }) => [
  { title: `${loaderData.title} · ${appName}` },
  { name: "description", content: loaderData.description },
];
export default function TermsOfService({ loaderData }: Route.ComponentProps) {
  return <PolicyPage name="terms-of-service" policy={loaderData} />;
}
