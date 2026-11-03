import { useEffect, useState } from "react";
import type { PolicyStatus } from "@/types/policies";
import { useApi } from "./api";
// Whether the terms of service and privacy policy are published (GET
// /api/policies), for the policy links below the homepage's, login's and
// register's card. Until the API answers, or when it can't, they're off:
// the prerendered homepage is built without asking, and shows the links
// once the browser knows.
export function usePolicies(): PolicyStatus {
  const api = useApi();
  const [status, setStatus] = useState<PolicyStatus>({ enabled: false });
  useEffect(() => {
    let current = true;
    api
      .policies()
      .show()
      .then(
        (answer) => current && setStatus(answer),
        () => {},
      );
    return () => {
      current = false;
    };
  }, [api]);
  return status;
}
