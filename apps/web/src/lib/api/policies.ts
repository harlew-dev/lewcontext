import type { PolicyStatus } from "@/types/policies";
import type { Http } from "./http";
import { path } from "./paths";
// The terms of service and privacy policy: whether they're published
// (POLICIES) and their current versions, which a sign-up sends back to
// agree to them. Public, signed in or out.
export const policyRoutes = {
  show: () => path<PolicyStatus>("/api/policies"),
};
// api.policies().show(): { enabled: false }, or the versions.
export function policies(http: Http) {
  return {
    show: () => http.get(policyRoutes.show()),
  };
}
