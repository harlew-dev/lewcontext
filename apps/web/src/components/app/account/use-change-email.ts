import { useState, type FormEvent } from "react";
import { useLocation, useNavigate } from "react-router";
import { useSWRConfig } from "swr";
import {
  ApiError,
  errorMessage,
  paths,
  useApi,
  type EmailChangeInput,
  type PendingEmailChange,
} from "@/lib/api";
import { loginPath } from "@/lib/routes";
import { useRenewSession, useSession } from "@/lib/session";
import type { Session } from "@/lib/types";
import { useAction } from "@/lib/use-action";
import { useResource } from "@/lib/use-resource";

export type ChangeEmailProps = {
  open: boolean;
  onClose: () => void;
  // Runs when the success screen is dismissed, with the new address.
  onChanged?: (email: string) => void | Promise<void>;
  // An address already typed elsewhere (account settings' email field): the
  // dialog starts at confirming account access for it and never mentions an
  // earlier request, which confirming replaces. Without one, an earlier
  // request resumes at its code, or a new address is asked for.
  initialEmail?: string;
};
type Step = "destination" | "proof" | "code";
// "done" is the success screen. It stays until the person dismisses it, and
// only then does onChanged run (a page may navigate away from the dialog).
type Message = { text: string };

// The shared dialog's workflow. Pending requests live on the server; entered
// factors live only in this mounted opening and its in-flight request.
export function useChangeEmail({
  open,
  onClose,
  onChanged,
  initialEmail,
}: ChangeEmailProps) {
  const api = useApi();
  const session = useSession();
  const renew = useRenewSession();
  const { mutate } = useSWRConfig();
  const navigate = useNavigate();
  const location = useLocation();
  const verification = session.emailVerification !== "off";
  const statusPath = paths.auth.emailChange().show();
  const status = useResource(open ? statusPath : null, undefined, {
    refreshInterval: (data) => (data?.pending?.state === "sending" ? 2000 : 0),
  });
  const pending = status.data?.pending;
  const [step, setStep] = useState<Step | null>(null);
  // Read once per opening: the page may clear its field while this closes.
  const [fresh] = useState(Boolean(initialEmail));
  const [email, setEmail] = useState(initialEmail ?? "");
  const [password, setPassword] = useState("");
  const [factor, setFactor] = useState("");
  const [code, setCode] = useState("");
  const [needsMfa, setNeedsMfa] = useState(Boolean(session.user.mfaEnabledAt));
  const [fields, setFields] = useState<Record<string, string>>({});
  const [message, setMessage] = useState<Message | null>(null);
  const [changedTo, setChangedTo] = useState<string | null>(null);
  // Cancelling ends on its own screen, with only a Close button.
  const [cancelled, setCancelled] = useState(false);
  const view = changedTo
    ? "done"
    : cancelled
      ? "cancelled"
      : (step ??
        (fresh ? "proof" : pending && verification ? "code" : "destination"));

  function clearSecrets() {
    setPassword("");
    setFactor("");
    setCode("");
  }
  function savePending(value: PendingEmailChange | null) {
    return mutate(
      [statusPath, session.user.id],
      { pending: value },
      { revalidate: false },
    );
  }
  async function completed(value: Session) {
    clearSecrets();
    renew(value);
    await savePending(null);
    // Refresh failures belong to their existing data notices. They must never
    // turn a committed email change into a failed/retryable mutation.
    void mutate(
      (key) => Array.isArray(key) && key[1] === session.user.id,
      undefined,
      { populateCache: false, throwOnError: false },
    );
    setChangedTo(value.user.email);
  }
  function showError(error: unknown): boolean {
    setPassword("");
    setFactor("");
    if (!(error instanceof ApiError)) return false;
    if (error.status === 401) {
      void navigate(loginPath(location.pathname + location.search), {
        replace: true,
      });
      return true;
    }
    if (
      error.status === 410 ||
      (error.status === 409 && error.details.pending !== true)
    ) {
      void savePending(null);
      setMessage({ text: errorMessage(error) });
      setCode("");
      setStep("destination");
      return true;
    }
    if (!error.field) return false;
    setFields({ [error.field]: errorMessage(error) });
    if (error.field === "email") setStep("destination");
    if (error.field === "currentPassword") {
      setNeedsMfa(false);
      setStep("proof");
    }
    if (error.field === "code" && view === "proof") setNeedsMfa(true);
    if (view === "code") setCode("");
    return true;
  }
  const request = useAction(
    async (input: EmailChangeInput) => {
      setPassword("");
      setFactor("");
      const result = await api.auth().emailChange().store(input);
      if (!("pending" in result)) return completed(result);
      await savePending(result.pending);
      setCode("");
      setStep("code");
    },
    { onError: showError },
  );
  const verify = useAction(
    async () => completed(await api.auth().emailChange().update(code)),
    { onError: showError },
  );
  const cancel = useAction(
    async () => {
      await api.auth().emailChange().destroy();
      await savePending(null);
      clearSecrets();
      setCancelled(true);
    },
    { onError: showError },
  );
  const finish = useAction(async () => {
    await onChanged?.(changedTo ?? "");
    onClose();
  });
  const busy = request.busy || verify.busy || cancel.busy || finish.busy;
  const loading = request.loading || verify.loading || cancel.loading;

  function resetIssues() {
    setFields({});
    setMessage(null);
    request.error.clear();
    verify.error.clear();
    cancel.error.clear();
  }
  function close() {
    if (busy) return;
    if (changedTo) return void finish.run();
    clearSecrets();
    onClose();
  }
  function resend() {
    if (busy || !pending) return;
    setEmail(pending.email);
    clearSecrets();
    resetIssues();
    setStep("proof");
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setFields({});
    setMessage(null);
    if (view === "done") return void finish.run();
    if (view === "destination") {
      if (email.trim().toLowerCase() === session.user.email.toLowerCase()) {
        setFields({
          email: "Choose an email address different from your current one.",
        });
      } else setStep("proof");
    } else if (view === "code") await verify.run();
    else
      await request.run(
        needsMfa
          ? { email: email.trim(), code: factor }
          : { email: email.trim(), currentPassword: password },
      );
  }
  const filled =
    view === "destination"
      ? Boolean(email.trim())
      : view === "proof"
        ? Boolean(needsMfa ? factor : password)
        : code.length === 6;
  return {
    view,
    pending,
    changedTo,
    finish,
    status,
    // A typed address needs nothing from the waiting request, which sending
    // replaces, so a slow or failed status read never holds it up.
    fresh,
    ready: fresh || status.data !== undefined,
    verification,
    email,
    setEmail,
    password,
    setPassword,
    factor,
    setFactor,
    code,
    setCode,
    needsMfa,
    fields,
    message,
    busy,
    loading,
    filled,
    request,
    verify,
    cancel,
    submit,
    close,
    resend,
    cancelChange: () => {
      if (!busy) {
        resetIssues();
        void cancel.run();
      }
    },
  };
}

export type ChangeEmailState = ReturnType<typeof useChangeEmail>;
