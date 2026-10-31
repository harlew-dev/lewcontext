import { useState } from "react";
import { useLocation, useSearchParams } from "react-router";
import { InvitationCallout } from "@/components/app/invitations/invitation-callout";
import { AuthLayout } from "@/components/layout/auth-layout";
import { Button } from "@/components/ui/button";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import { Field, Label } from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Logo } from "@/components/ui/logo";
import { Strong, Text, TextButton, TextLink } from "@/components/ui/text";
import { CodeField } from "@/components/app/mfa/mfa-setup";
import { ApiError, errorMessage, needsMfa, useApi } from "@/lib/api";
import type { Credentials } from "@/lib/api";
import { authPageState, authPath, invitationToken, routes } from "@/lib/routes";
import { guestLoader } from "@/lib/loaders";
import { useAuthSubmit } from "@/lib/use-auth-submit";
import { useInvitation } from "@/lib/use-invitation";
import type { Route } from "./+types/login";
// Signed-in visitors go on to ?next= or /app.
export function clientLoader({ request }: Route.ClientLoaderArgs) {
  return guestLoader(request);
}
export default function Login() {
  // After a password reset, the reset page passes the account's email; after
  // deleting an account, settings says so.
  const arrived = authPageState(useLocation().state);
  const [email, setEmail] = useState(arrived.email ?? "");
  const [password, setPassword] = useState("");
  const api = useApi();
  const [search] = useSearchParams();
  // Coming from an invitation link: say so, and fill in its email.
  const invitation = useInvitation(invitationToken(search.get("next")));
  const [prefilled, setPrefilled] = useState(false);
  if (invitation.kind === "live" && !prefilled) {
    setPrefilled(true);
    if (email === "") setEmail(invitation.invitation.email);
  }
  // With two-factor authentication on, the right password leads to the code
  // step; when that ends (expired, too many wrong codes) the password is
  // needed again, with the reason.
  const [step, setStep] = useState<"password" | "code">("password");
  const [restarted, setRestarted] = useState("");
  async function signIn(credentials: Credentials) {
    if (!needsMfa(await api.auth().login(credentials))) return true;
    setPassword("");
    setRestarted("");
    setStep("code");
    return false;
  }
  const { loading, error, submit } = useAuthSubmit(signIn);
  if (step === "code")
    return (
      <AuthLayout>
        <CodeStep
          onRestart={(reason) => {
            setRestarted(reason);
            setStep("password");
          }}
        />
      </AuthLayout>
    );
  return (
    <AuthLayout>
      <form
        onSubmit={(event) => submit(event, { email, password })}
        className="grid w-full max-w-sm grid-cols-1 gap-8"
      >
        <Logo href={routes.home} />
        <Heading>Sign in to your account</Heading>
        <InvitationCallout
          check={invitation}
          ending="Sign in to accept."
          dead="This invitation has expired or was revoked. You can still sign in."
        />
        {arrived.passwordReset && !error.message && (
          <Notice variant="success">
            Password updated. Sign in with your new password.
          </Notice>
        )}
        {arrived.accountDeleted && !error.message && (
          <Notice variant="success">Your account has been deleted.</Notice>
        )}
        {restarted && !error.message && (
          <Notice variant="error">{restarted}</Notice>
        )}
        <ErrorNotice error={error} />
        <Field disabled={loading}>
          <Label>Email</Label>
          <Input
            type="email"
            name="email"
            autoComplete="email"
            required
            maxLength={254}
            value={email}
            onChange={(e) => setEmail(e.currentTarget.value)}
          />
        </Field>
        <Field disabled={loading}>
          <Label>Password</Label>
          <Input
            type="password"
            name="password"
            autoComplete="current-password"
            required
            maxLength={128}
            value={password}
            onChange={(e) => setPassword(e.currentTarget.value)}
          />
        </Field>
        {/* The email goes along in navigation state, not the URL. */}
        <Text>
          <TextLink href={routes.forgotPassword} state={{ email }}>
            <Strong>Forgot password?</Strong>
          </TextLink>
        </Text>
        <Button type="submit" className="w-full" loading={loading}>
          Sign in
        </Button>
        <Text>
          Don’t have an account?{" "}
          <TextLink
            href={authPath(routes.register, search.get("next"))}
            state={arrived.invitation ? { invitation: true } : undefined}
          >
            <Strong>Sign up</Strong>
          </TextLink>
        </Text>
      </form>
    </AuthLayout>
  );
}
// The second step with two-factor authentication on: a code from the
// authenticator app, or a recovery code. The challenge lives in a cookie, so
// a refresh starts over at the password. A 401 means it ended: back to the
// password with the API's reason.
function CodeStep({ onRestart }: { onRestart: (reason: string) => void }) {
  const api = useApi();
  const [recovery, setRecovery] = useState(false);
  const [code, setCode] = useState("");
  const [fieldError, setFieldError] = useState<string>();
  const { loading, error, submit } = useAuthSubmit(
    (value: string) => api.auth().loginMfa(value),
    undefined,
    (e) => {
      if (e instanceof ApiError && e.status === 401) {
        onRestart(errorMessage(e));
        return true;
      }
      if (e instanceof ApiError && e.field === "code") {
        setFieldError(errorMessage(e));
        return true;
      }
    },
  );
  function switchMode() {
    setRecovery(!recovery);
    setCode("");
    setFieldError(undefined);
  }
  return (
    <form
      onSubmit={(event) => {
        setFieldError(undefined);
        return submit(event, code);
      }}
      className="grid w-full max-w-sm grid-cols-1 gap-8"
    >
      <Logo href={routes.home} />
      <Heading>Two-factor authentication</Heading>
      <Text>
        {recovery
          ? "Enter one of your recovery codes. Each one works once."
          : "Enter the code from your authenticator app."}
      </Text>
      <ErrorNotice error={error} />
      <CodeField
        key={recovery ? "recovery" : "app"}
        label={recovery ? "Recovery code" : "Authentication code"}
        value={code}
        onChange={setCode}
        error={fieldError}
        disabled={loading}
        recovery={recovery}
        autoFocus
      />
      <Button type="submit" className="w-full" loading={loading}>
        Verify
      </Button>
      <div className="space-y-2">
        <Text>
          <TextButton onClick={switchMode} disabled={loading}>
            <Strong>
              {recovery
                ? "Use your authenticator app instead"
                : "Lost your device? Use a recovery code"}
            </Strong>
          </TextButton>
        </Text>
        <Text>
          <TextButton onClick={() => onRestart("")} disabled={loading}>
            Sign in as someone else
          </TextButton>
        </Text>
      </div>
    </form>
  );
}
