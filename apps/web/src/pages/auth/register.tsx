import { useState } from "react";
import { useSearchParams } from "react-router";
import { AuthLayout } from "@/components/layout/auth-layout";
import { Button } from "@/components/ui/button";
import { ErrorNotice } from "@/components/ui/feedback";
import { Description, Field, Label } from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Logo } from "@/components/ui/logo";
import { Strong, Text, TextLink } from "@/components/ui/text";
import { useApi } from "@/lib/api";
import { authPath, routes } from "@/lib/routes";
import { guestLoader } from "@/lib/loaders";
import { useAuthSubmit } from "@/lib/use-auth-submit";
import type { Route } from "./+types/register";
// Signed-in visitors go on to ?next= or /app.
export function clientLoader({ request }: Route.ClientLoaderArgs) {
  return guestLoader(request);
}
export default function Register() {
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const api = useApi();
  const [search] = useSearchParams();
  // A new account has no workspace yet, so it always starts at onboarding.
  const { loading, error, submit } = useAuthSubmit(
    api.auth().register,
    routes.onboarding,
  );
  return (
    <AuthLayout>
      <form
        onSubmit={(event) => submit(event, { name, email, password })}
        className="grid w-full max-w-sm grid-cols-1 gap-8"
      >
        <Logo />
        <Heading>Create your account</Heading>
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
          <TextLink href={authPath(routes.login, search.get("next"))}>
            <Strong>Sign in</Strong>
          </TextLink>
        </Text>
      </form>
    </AuthLayout>
  );
}
