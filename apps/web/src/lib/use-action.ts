import { useRef, useState } from "react";
import { useActionError } from "./use-action-error";
import { useDelayedLoading } from "./use-delayed-loading";
export type ActionResult<T> = { ok: true; value: T } | { ok: false };
type ActionOptions = {
  // Handles a failure itself (a toast, or an error shown on one field).
  // Return true to keep it out of `error`, which also clears the last one.
  onError?: (error: unknown) => boolean | void;
};
// A form submission or button action: `busy` from the moment it runs until
// it settles (for logic, e.g. not closing a dialog mid-save), `loading` once
// it has run for 200 ms and for at least 400 ms (for <Button loading> and
// disabled fields, so a quick request doesn't flicker; useDelayedLoading),
// the last failure in `error` (for <ErrorNotice>), and `run`, which ignores
// calls while one is in flight, so a double click submits once. `run` awaits everything the action awaits,
// navigation included, and never throws: it resolves { ok: true, value } or
// { ok: false }.
//   const save = useAction((input: ProjectInput) => api.projects(id).store(input));
//   const result = await save.run(form);
export function useAction<A extends unknown[], T>(
  action: (...args: A) => Promise<T>,
  { onError }: ActionOptions = {},
) {
  const [busy, setBusy] = useState(false);
  const loading = useDelayedLoading(busy);
  // A ref, not state: two clicks in the same tick both see the same state.
  const running = useRef(false);
  const error = useActionError();
  async function run(...args: A): Promise<ActionResult<T>> {
    if (running.current) return { ok: false };
    running.current = true;
    setBusy(true);
    try {
      const value = await action(...args);
      error.clear();
      return { ok: true, value };
    } catch (e) {
      if (onError?.(e) === true) error.clear();
      else error.fail(e);
      return { ok: false };
    } finally {
      running.current = false;
      setBusy(false);
    }
  }
  return { run, busy, loading, error };
}
