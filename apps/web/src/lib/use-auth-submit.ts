import { useState, type FormEvent } from "react";
import { useRouter } from "next/router";
import { request, errorMessage } from "./api";
import { routes } from "./routes";
// Shared by the sign-in and registration pages: posts the credentials, then
// opens the dashboard; keeps the form disabled while the request is pending.
export function useAuthSubmit(endpoint: string) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent, body: Record<string, string>) {
    event.preventDefault();
    setError("");
    setBusy(true);
    try {
      await request(endpoint, { method: "POST", body: JSON.stringify(body) });
      await router.push(routes.dashboard);
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  }
  return { busy, error, submit };
}
