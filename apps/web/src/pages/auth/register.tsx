import { useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";
import { InvitationCallout } from "@/components/app/invitations/invitation-callout";
import { AuthLayout, BackButton } from "@/components/layout/auth-layout";
import { Button } from "@/components/ui/button";
import { ErrorNotice } from "@/components/ui/feedback";
import { Description, Field, Label } from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Logo } from "@/components/ui/logo";
import { Strong, Text, TextLink } from "@/components/ui/text";
import { needsVerification, useApi, type Registration } from "@/lib/api";
import { authPageState, authPath, invitationToken, routes } from "@/lib/routes";
import { guestLoader } from "@/lib/loaders";
import { useAuthSubmit } from "@/lib/use-auth-submit";
import { useInvitation } from "@/lib/use-invitation";
import type { Route } from "./+types/register";
// Signed-in visitors go on to ?next= or /app.
export function clientLoader({ request }: Route.ClientLoaderArgs) {
  return guestLoader(request);
}
export default function Register() {
  const arrived = authPageState(useLocation().state);
  const [name, setName] = useState("");
  const [email, setEmail] = useState(arrived.email ?? "");
  const [password, setPassword] = useState("");
  const api = useApi();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  // Coming from an invitation link: say so, and fill in its email.
  const token = invitationToken(search.get("next"));
  const invitation = useInvitation(token);
  const [prefilled, setPrefilled] = useState(false);
  if (invitation.kind === "live" && !prefilled) {
    setPrefilled(true);
    if (email === "") setEmail(invitation.invitation.email);
  }
  // With email verification on there's no account yet: the code page
  // finishes, keeping ?next= and the invitation state.
  async function signUp(registration: Registration) {
    const result = await api.auth().register(registration);
    if (!needsVerification(result)) return true;
    await navigate(authPath(routes.registerVerify, search.get("next")), {
      state: arrived.invitation ? { invitation: true } : undefined,
    });
    return false;
  }
  // A new account has no workspace yet, so it starts at onboarding, unless it
  // came to accept an invitation: then it goes back to the link.
  const { loading, error, submit } = useAuthSubmit(
    signUp,
    token ? undefined : routes.onboarding,
  );
  return (
    <AuthLayout>
      <form
        onSubmit={(event) => submit(event, { name, email, password })}
        className="grid w-full max-w-sm grid-cols-1 gap-8"
      >
        <Logo href={routes.home} />
        {/* Back to sign in, keeping ?next= and the invitation. */}
        <BackButton
          href={authPath(routes.login, search.get("next"))}
          state={arrived.invitation ? { invitation: true } : undefined}
        />
        <Heading>Create your account</Heading>
        <InvitationCallout
          check={invitation}
          ending="Create an account with this email to accept."
          dead="This invitation has expired or was revoked. You can still create an account."
        />
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
          <Label>Full name</Label>
          <Input
            name="name"
            autoComplete="name"
            required
            maxLength={80}
            value={name}
            onChange={(e) => setName(e.currentTarget.value)}
          />
        </Field>
        <Field disabled={loading}>
          <Label>Password</Label>
          <Description>
            Use at least 12 characters. A passphrase works well.
          </Description>
          <Input
            type="password"
            name="password"
            autoComplete="new-password"
            required
            minLength={12}
            maxLength={128}
            value={password}
            onChange={(e) => setPassword(e.currentTarget.value)}
          />
        </Field>
        <Button type="submit" className="w-full" loading={loading}>
          Create account
        </Button>
        <Text>
          Already have an account?{" "}
          <TextLink
            href={authPath(routes.login, search.get("next"))}
            state={arrived.invitation ? { invitation: true } : undefined}
          >
            <Strong>Sign in</Strong>
          </TextLink>
        </Text>
      </form>
    </AuthLayout>
  );
}
