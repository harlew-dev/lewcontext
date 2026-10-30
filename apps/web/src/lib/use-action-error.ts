import { useState } from "react";
import { errorMessage } from "./api";
// The error from the last attempt of a form or action (useAction keeps one).
// It stays on screen while the next attempt runs and is only replaced by that
// attempt's result, so the layout doesn't jump on every submit. `key` changes
// with each failure; <ErrorNotice> uses it so a repeated message is announced
// again.
export type ActionError = { message: string; key: number };
export function useActionError() {
  const [state, setState] = useState({ message: "", key: 0 });
  return {
    message: state.message,
    key: state.key,
    fail: (error: unknown) =>
      setState((last) => ({
        message: errorMessage(error),
        key: last.key + 1,
      })),
    clear: () => setState((last) => ({ ...last, message: "" })),
  };
}
