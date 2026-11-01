import { useEffect, useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router";
import {
  NewPasswordFields,
  newPasswordProblems,
} from "@/components/app/account/password-fields";
import { AuthLayout, BackButton } from "@/components/layout/auth-layout";
import { Button } from "@/components/ui/button";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import { Heading, HeadingGroup } from "@/components/ui/heading";
import { Logo } from "@/components/ui/logo";
import { Text } from "@/components/ui/text";
import { ApiError, errorMessage, useApi } from "@/lib/api";
import { routes, type AuthPageState } from "@/lib/routes";
import { useAction } from "@/lib/use-action";
import { useDelayedLoading } from "@/lib/use-delayed-loading";
// The page a reset link opens: /auth/forgot-password/<token>. It checks the
// link first, so an expired one says so before the user types a password,
// then sets the new password and sends the user to sign in with it.
//
// Signed-in visitors can use it too (no guestLoader): resetting signs out
// every session anyway. The token never leaves this page except in the JSON
// bodies of the check and reset requests.
type LinkState =
  | { kind: "checking" }
  | { kind: "live" }
  | { kind: "dead" }
  | { kind: "unreachable"; message: string };
// The shape auth.Token makes in Go: 32 random bytes as 43 base64url characters.
const tokenShape = /^[A-Za-z0-9_-]{43}$/;
// A link the API refused: unknown, expired, replaced by a newer one, or used.
function deadLink(error: unknown): boolean {
  return (
    error instanceof ApiError && error.status === 422 && error.field === "token"
  );
}
export default function ResetPassword() {
  const token = useParams().token ?? "";
  const { state, retry, markDead } = useLinkCheck(token);
  const checking = useDelayedLoading(state.kind === "checking");
  return (
    <AuthLayout>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <Logo href={routes.home} />
        <BackButton href={routes.login} />
        {state.kind === "checking" &&
          (checking ? <Text role="status">Checking your link…</Text> : null)}
        {state.kind === "live" && (
          <NewPasswordForm token={token} onDead={markDead} />
        )}
        {state.kind === "dead" && <DeadLink />}
        {state.kind === "unreachable" && (
          <>
            <Heading>Choose a new password</Heading>
            <Notice variant="error" onRetry={retry}>
              {state.message}
            </Notice>
          </>
        )}
      </div>
    </AuthLayout>
  );
}
// Asks the API whether the link still works, again on retry.
function useLinkCheck(token: string) {
  const api = useApi();
  const wellFormed = tokenShape.test(token);
  const [state, setState] = useState<LinkState>(
    wellFormed ? { kind: "checking" } : { kind: "dead" },
  );
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!wellFormed) return;
    let current = true;
    api
      .auth()
      .checkPasswordReset(token)
      .then(
        () => current && setState({ kind: "live" }),
        (error: unknown) =>
          current &&
          setState(
            deadLink(error)
              ? { kind: "dead" }
              : { kind: "unreachable", message: errorMessage(error) },
          ),
      );
    return () => {
      current = false;
    };
  }, [api, token, wellFormed, attempt]);
  return {
    state,
    retry: () => {
      setState({ kind: "checking" });
      setAttempt(attempt + 1);
    },
    markDead: () => setState({ kind: "dead" }),
  };
}
function NewPasswordForm({
  token,
  onDead,
}: {
  token: string;
  onDead: () => void;
}) {
  const api = useApi();
  const navigate = useNavigate();
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const reset = useAction(
    async () => {
      const { email } = await api
        .auth()
        .resetPassword({ token, newPassword: password });
      const state: AuthPageState = { email, passwordReset: true };
      await navigate(routes.login, { replace: true, state });
    },
    {
      // A link that died meanwhile (used in another tab, or expired) shows
      // the expired page; a refused password shows under its field.
      onError: (e) => {
        if (deadLink(e)) {
          onDead();
          return true;
        }
        if (e instanceof ApiError && e.field === "newPassword") {
          setFieldErrors({ newPassword: errorMessage(e) });
          return true;
        }
        return false;
      },
    },
  );
  async function submit(event: FormEvent) {
    event.preventDefault();
    const problems = newPasswordProblems(password, confirmation);
    setFieldErrors(problems);
    if (Object.keys(problems).length > 0) return;
    await reset.run();
  }
  return (
    <form onSubmit={submit} className="grid grid-cols-1 gap-8">
      <HeadingGroup>
        <Heading>Choose a new password</Heading>
        <Text>
          Every session on your account will be signed out, and you’ll sign in
          again with the new password.
        </Text>
      </HeadingGroup>
      <ErrorNotice error={reset.error} />
      <NewPasswordFields
        password={password}
        confirmation={confirmation}
        onPasswordChange={setPassword}
        onConfirmationChange={setConfirmation}
        errors={fieldErrors}
        disabled={reset.loading}
      />
      <Button type="submit" className="w-full" loading={reset.loading}>
        Set new password
      </Button>
    </form>
  );
}
function DeadLink() {
  return (
    <>
      <Heading>This link has expired</Heading>
      <Notice variant="error">
        Reset links work once, for 30 minutes, and only the newest one you asked
        for works.
      </Notice>
      <Button href={routes.forgotPassword} className="w-full">
        Request a new link
      </Button>
    </>
  );
}
