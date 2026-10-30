import { useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { returnPath, routes } from "./routes";
import { useActionError } from "./use-action-error";
// Shared by the sign-in and registration pages: sends the form with `signIn`
// (api.auth().login or .register), then opens `destination` when given (a new
// account goes to onboarding), else ?next= or /app (the newest workspace);
// keeps the form disabled while the request is pending. A failed attempt's error
// stays until the next attempt fails or the page changes.
export function useAuthSubmit<B>(
  signIn: (body: B) => Promise<unknown>,
  destination?: string,
) {
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const [busy, setBusy] = useState(false);
  const error = useActionError();
  async function submit(event: FormEvent, body: B) {
    event.preventDefault();
    setBusy(true);
    try {
      await signIn(body);
      await navigate(
        destination ?? returnPath(search.get("next")) ?? routes.app,
      );
    } catch (e) {
      error.fail(e);
      setBusy(false);
    }
  }
  return { busy, error, submit };
}
