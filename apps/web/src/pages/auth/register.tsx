import { useState } from "react";
import type { GetServerSidePropsContext } from "next";
import { useRouter } from "next/router";
import { AuthLayout } from "@/components/layout/auth-layout";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Description, Field, Label } from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Logo } from "@/components/ui/logo";
import { Strong, Text, TextLink } from "@/components/ui/text";
import { useApi } from "@/lib/api";
import { authPath, routes } from "@/lib/routes";
import { guestProps } from "@/lib/server";
import { useAuthSubmit } from "@/lib/use-auth-submit";
// Signed-in visitors go on to ?next= or the dashboard. Request rendering also
// supplies a fresh CSP nonce and per-request Styletron styles.
export function getServerSideProps(context: GetServerSidePropsContext) {
  return guestProps(context, { redirectSignedIn: true });
}
export default function Register() {
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const api = useApi();
  const router = useRouter();
  const { busy, error, submit } = useAuthSubmit(api.auth().register);
  return (
    <AuthLayout>
      <form
        onSubmit={(event) => submit(event, { name, email, password })}
        className="grid w-full max-w-sm grid-cols-1 gap-8"
      >
        <Logo />
        <Heading>Create your account</Heading>
        {error.message && (
          <Notice key={error.key} variant="error">
            {error.message}
          </Notice>
        )}
        <Field disabled={busy}>
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
        <Field disabled={busy}>
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
        <Field disabled={busy}>
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
        <Button type="submit" className="w-full" loading={busy}>
          Create account
        </Button>
        <Text>
          Already have an account?{" "}
          <TextLink href={authPath(routes.login, router.query.next)}>
            <Strong>Sign in</Strong>
          </TextLink>
        </Text>
      </form>
    </AuthLayout>
  );
}
