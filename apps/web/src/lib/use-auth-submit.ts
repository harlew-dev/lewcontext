import type { FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { returnPath, routes } from "./routes";
import { useAction } from "./use-action";
// Shared by the sign-in and registration pages: sends the form with `signIn`
// (api.auth().login or .register), then opens `destination` when given (a new
// account goes to onboarding), else ?next= or /app (the newest workspace). The
// form stays loading until that page has loaded. A failed attempt's error
// stays until the next attempt fails or the page changes.
export function useAuthSubmit<B>(
  signIn: (body: B) => Promise<unknown>,
  destination?: string,
) {
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const action = useAction(async (body: B) => {
    await signIn(body);
    await navigate(destination ?? returnPath(search.get("next")) ?? routes.app);
  });
  async function submit(event: FormEvent, body: B) {
    event.preventDefault();
    await action.run(body);
  }
  return { loading: action.loading, error: action.error, submit };
}
