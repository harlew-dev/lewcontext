import { useState, type FormEvent } from "react";
import { useLocation } from "react-router";
import { AuthLayout, BackButton } from "@/components/layout/auth-layout";
import { Button } from "@/components/ui/button";
import { ErrorNotice } from "@/components/ui/feedback";
import { Field, Label } from "@/components/ui/fieldset";
import { Heading, HeadingGroup } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Logo } from "@/components/ui/logo";
import { Strong, Text } from "@/components/ui/text";
import { useApi } from "@/lib/api";
import { guestLoader } from "@/lib/loaders";
import { authPageState, routes } from "@/lib/routes";
import { useAction } from "@/lib/use-action";
import type { Route } from "./+types/forgot-password";
// Signed-in visitors go on to /app; they can change their password in
// account settings.
export function clientLoader({ request }: Route.ClientLoaderArgs) {
  return guestLoader(request);
}
// Asks for a reset link. The API answers the same whether or not an account
// uses the email, so this page does too: it never says whether one exists.
export default function ForgotPassword() {
  // The login page passes the email typed there.
  const arrived = authPageState(useLocation().state);
  const [email, setEmail] = useState(arrived.email ?? "");
  const [sentTo, setSentTo] = useState<string | null>(null);
  const api = useApi();
  const request = useAction(() => api.auth().requestPasswordReset(email));
  async function submit(event: FormEvent) {
    event.preventDefault();
    if ((await request.run()).ok) setSentTo(email.trim());
  }
  if (sentTo !== null)
    return <LinkSent email={sentTo} onTryAgain={() => setSentTo(null)} />;
  return (
    <AuthLayout>
      <form
        onSubmit={submit}
        className="grid w-full max-w-sm grid-cols-1 gap-8"
      >
        <Logo href={routes.home} />
        <BackButton href={routes.login} state={{ email }} />
        <HeadingGroup>
          <Heading>Reset your password</Heading>
          <Text>
            Enter your account’s email and we’ll send you a link to choose a new
            password.
          </Text>
        </HeadingGroup>
        <ErrorNotice error={request.error} />
        <Field disabled={request.loading}>
          <Label>Email</Label>
          <Input
            type="email"
            name="email"
            autoComplete="email"
            autoFocus
            required
            maxLength={254}
            value={email}
            onChange={(e) => setEmail(e.currentTarget.value)}
          />
        </Field>
        <Button type="submit" className="w-full" loading={request.loading}>
          Send reset link
        </Button>
      </form>
    </AuthLayout>
  );
}
function LinkSent({
  email,
  onTryAgain,
}: {
  email: string;
  onTryAgain: () => void;
}) {
  return (
    <AuthLayout>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <Logo href={routes.home} />
        {/* Back to the form, with the email still in it, to try again. */}
        <BackButton onClick={onTryAgain} />
        {/* Announced when it replaces the form. */}
        <HeadingGroup role="status">
          <Heading>Check your email</Heading>
          <Text>
            If an account uses this email, we’ve sent it a reset link:
          </Text>
          <Text className="break-all">
            <Strong>{email}</Strong>
          </Text>
        </HeadingGroup>
        <Text>
          The link works once and expires in 30 minutes. Nothing after a few
          minutes? Check your spam folder, or go back and try again.
        </Text>
      </div>
    </AuthLayout>
  );
}
