import { useState } from "react";
import { useLocation, useSearchParams } from "react-router";
import { InvitationCallout } from "@/components/app/invitations/invitation-callout";
import { AuthLayout, BackButton } from "@/components/layout/auth-layout";
import { Button } from "@/components/ui/button";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import { Description, Field, Label } from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Logo } from "@/components/ui/logo";
import { Strong, Text, TextLink } from "@/components/ui/text";
import { CodeStep } from "@/components/app/mfa/code-step";
import { PolicyLinks } from "@/components/app/policies/policy-links";
import { needsMfa, useApi } from "@/lib/api";
import type { Credentials } from "@/lib/api";
import { authPageState, authPath, invitationToken, routes } from "@/lib/routes";
import { guestLoader } from "@/lib/loaders";
import { useAuthSubmit } from "@/lib/use-auth-submit";
import { useInvitation } from "@/lib/use-invitation";
import { usePolicies } from "@/lib/use-policies";
import type { Route } from "./+types/index";
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
  const policies = usePolicies();
  if (step === "code")
    return (
      <AuthLayout>
        <CodeStep
          verify={(code) => api.auth().loginMfa(code)}
          onRestart={(reason) => {
            setRestarted(reason);
            setStep("password");
          }}
        />
      </AuthLayout>
    );
  return (
    <AuthLayout footer={policies.enabled && <PolicyLinks />}>
      <form
        onSubmit={(event) => submit(event, { email, password })}
        className="grid w-full max-w-sm grid-cols-1 gap-8"
      >
        <Logo href={routes.home} />
        <BackButton href={routes.home} />
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
        {/* From the "You already have an account" email, or a sign-up whose
            code found one. Fixed text: nothing from the URL is shown. */}
        {search.get("notice") === "account-exists" && !error.message && (
          <Notice variant="info">
            You already have an account. Sign in here, or{" "}
            <TextLink href={routes.forgotPassword} state={{ email }}>
              reset your password
            </TextLink>
            .
          </Notice>
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
          {/* Under the input, as its description. The email goes along in
              navigation state, not the URL. */}
          <Description>
            <TextLink href={routes.forgotPassword} state={{ email }}>
              <Strong>Forgot password?</Strong>
            </TextLink>
          </Description>
        </Field>
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
