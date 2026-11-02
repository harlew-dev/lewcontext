import { useState } from "react";
import { errorMessage, requestIdOf } from "./api";
// The error from the last attempt of a form or action (useAction keeps one).
// It stays on screen while the next attempt runs and is only replaced by that
// attempt's result, so the layout doesn't jump on every submit. `key` changes
// with each failure; <ErrorNotice> uses it so a repeated message is announced
// again. `requestId` is a 500's reference, shown under the message.
export type ActionError = { message: string; key: number; requestId?: string };
export function useActionError() {
  const [state, setState] = useState<ActionError>({ message: "", key: 0 });
  return {
    message: state.message,
    key: state.key,
    requestId: state.requestId,
    fail: (error: unknown) =>
      setState((last) => ({
        message: errorMessage(error),
        key: last.key + 1,
        requestId: requestIdOf(error),
      })),
    clear: () =>
      setState((last) => ({ ...last, message: "", requestId: undefined })),
  };
}
