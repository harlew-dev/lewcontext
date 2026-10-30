import { useState, type FormEvent } from "react";
import { useRouter } from "next/router";
import { returnPath, routes } from "./routes";
import { useActionError } from "./use-action-error";
// Shared by the sign-in and registration pages: sends the form with `signIn`
// (api.auth().login or .register), then opens ?next= or the dashboard; keeps
// the form disabled while the request is pending. A failed attempt's error
// stays until the next attempt fails or the page changes.
export function useAuthSubmit<B>(signIn: (body: B) => Promise<unknown>) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const error = useActionError();
  async function submit(event: FormEvent, body: B) {
    event.preventDefault();
    setBusy(true);
    try {
      await signIn(body);
      await router.push(returnPath(router.query.next) ?? routes.dashboard);
    } catch (e) {
      error.fail(e);
      setBusy(false);
    }
  }
  return { busy, error, submit };
}
