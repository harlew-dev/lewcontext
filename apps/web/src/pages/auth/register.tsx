import { useState } from "react";
import { AuthLayout } from "@/components/layout/auth-layout";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Description, Field, Label } from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Logo } from "@/components/ui/logo";
import { Strong, Text, TextLink } from "@/components/ui/text";
import { apiRoutes, routes } from "@/lib/routes";
import { useAuthSubmit } from "@/lib/use-auth-submit";
// Request rendering supplies a fresh CSP nonce and per-request Styletron styles.
export const getServerSideProps = async () => ({ props: {} });
export default function Register() {
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const { busy, error, submit } = useAuthSubmit(apiRoutes.register);
  return (
    <AuthLayout>
      <form
        onSubmit={(event) => submit(event, { name, email, password })}
        className="grid w-full max-w-sm grid-cols-1 gap-8"
      >
        <Logo />
        <Heading>Create your account</Heading>
        {error && <Notice variant="error">{error}</Notice>}
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
          <TextLink href={routes.login}>
            <Strong>Sign in</Strong>
          </TextLink>
        </Text>
      </form>
    </AuthLayout>
  );
}
