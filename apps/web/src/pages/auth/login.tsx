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
import { Strong, Text, TextLink } from "@/components/ui/text";
import { useApi } from "@/lib/api";
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
  const { loading, error, submit } = useAuthSubmit(api.auth().login);
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
