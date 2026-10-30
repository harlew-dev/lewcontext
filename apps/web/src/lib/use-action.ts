import { useRef, useState } from "react";
import { useActionError } from "./use-action-error";
export type ActionResult<T> = { ok: true; value: T } | { ok: false };
type ActionOptions = {
  // Handles a failure itself (a toast, or an error shown on one field).
  // Return true to keep it out of `error`, which also clears the last one.
  onError?: (error: unknown) => boolean | void;
};
// A form submission or button action: `loading` while it runs (for
// <Button loading> and disabled fields), the last failure in `error` (for
// <ErrorNotice>), and `run`, which ignores calls while one is in flight, so a
// double click submits once. `run` awaits everything the action awaits,
// navigation included, and never throws: it resolves { ok: true, value } or
// { ok: false }.
//   const save = useAction((input: ProjectInput) => api.projects(id).create(input));
//   const result = await save.run(form);
export function useAction<A extends unknown[], T>(
  action: (...args: A) => Promise<T>,
  { onError }: ActionOptions = {},
) {
  const [loading, setLoading] = useState(false);
  // A ref, not state: two clicks in the same tick both see the same state.
  const running = useRef(false);
  const error = useActionError();
  async function run(...args: A): Promise<ActionResult<T>> {
    if (running.current) return { ok: false };
    running.current = true;
    setLoading(true);
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
      setLoading(false);
    }
  }
  return { run, loading, error };
}
