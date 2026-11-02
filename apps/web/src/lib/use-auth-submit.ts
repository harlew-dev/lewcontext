import type { FormEvent } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";
import { authPageState, returnPath, routes } from "./routes";
import { useAction } from "./use-action";
// Shared by the sign-in and registration pages: sends the form with `signIn`
// (api.auth().login or .register), then opens `destination` when given (a new
// account goes to onboarding), else ?next= or /app (the newest workspace). The
// form stays busy (and loading) until that page has loaded. A failed attempt's error
// stays until the next attempt fails or the page changes, unless onError
// handles it (returning true, as useAction's). A sign-in that needs another
// step first (a second factor) returns false and stays on the page. Arriving
// from the invitation page's buttons, it passes that on, so the invitation is
// accepted as soon as the visitor is signed in.
export function useAuthSubmit<B>(
  signIn: (body: B) => Promise<unknown>,
  destination?: string,
  onError?: (error: unknown) => boolean | void,
) {
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const { invitation } = authPageState(useLocation().state);
  const action = useAction(
    async (body: B) => {
      if ((await signIn(body)) === false) return;
      await navigate(
        destination ?? returnPath(search.get("next")) ?? routes.app,
        invitation ? { state: { invitation } } : undefined,
      );
    },
    { onError },
  );
  async function submit(event: FormEvent, body: B) {
    event.preventDefault();
    await action.run(body);
  }
  return {
    busy: action.busy,
    loading: action.loading,
    error: action.error,
    submit,
  };
}
