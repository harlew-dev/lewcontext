import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { CodeStep } from "@/components/app/mfa/code-step";
import { MfaScanStep, RecoveryCodeList } from "@/components/app/mfa/mfa-setup";
import { AuthLayout } from "@/components/layout/auth-layout";
import { ServiceUnavailable } from "@/components/layout/status-page";
import { Button } from "@/components/ui/button";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import { Field, Label } from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Logo } from "@/components/ui/logo";
import { Text } from "@/components/ui/text";
import { ApiError, errorMessage, type MfaSetup } from "@/lib/api";
import {
  needsMfaSetup,
  useAdminApi,
  type AdminSetupDone,
} from "@/lib/api/admin";
import { AdminProvider, adminLoginLoader, useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { appName } from "@/lib/app";
import { routes } from "@/lib/routes";
import { useAction } from "@/lib/use-action";
import type { Route } from "./+types/index";
// The admin sign-in page, /admin/<key>. Its loader asks the API about the
// key first, so a wrong one shows the ordinary not-found page, never this
// form; a signed-in admin goes on to Users. Every sign-in takes the password
// and then a second factor; an admin without one sets it up here.
export function clientLoader({ params }: Route.ClientLoaderArgs) {
  return adminLoginLoader(params.key);
}
export const meta: Route.MetaFunction = () => [
  { title: `Admin sign-in · ${appName}` },
  { name: "robots", content: "noindex, nofollow" },
];
export default function AdminLogin({
  loaderData,
  params,
}: Route.ComponentProps) {
  if ("unavailable" in loaderData)
    return <ServiceUnavailable requestId={loaderData.requestId} />;
  return (
    <AdminProvider adminKey={params.key} session={null}>
      <AuthLayout>
        <AdminSignIn />
      </AuthLayout>
    </AdminProvider>
  );
}
// The password, then the code (MFA on) or the setup (MFA off, inside the
// window `admin grant` opened). When the second step's challenge ends, it's
// back to the password with the reason.
function AdminSignIn() {
  const key = useAdminKey();
  const [step, setStep] = useState<"password" | "code" | "setup">("password");
  const [email, setEmail] = useState("");
  const [setup, setSetup] = useState<MfaSetup | null>(null);
  const [restarted, setRestarted] = useState("");
  function restart(reason: string) {
    setRestarted(reason);
    setSetup(null);
    setStep("password");
  }
  if (step === "code")
    return (
      <AdminCodeStep
        destination={adminRoutes.users.index(key)}
        onRestart={restart}
      />
    );
  if (step === "setup" && setup)
    return <AdminSetup email={email} setup={setup} onRestart={restart} />;
  return (
    <PasswordStep
      email={email}
      onEmailChange={setEmail}
      restarted={restarted}
      onNext={(next) => {
        setRestarted("");
        if (next.setup) setSetup(next.setup);
        setStep(next.setup ? "setup" : "code");
      }}
    />
  );
}
function PasswordStep({
  email,
  onEmailChange,
  restarted,
  onNext,
}: {
  email: string;
  onEmailChange: (email: string) => void;
  restarted: string;
  onNext: (next: { setup?: MfaSetup }) => void;
}) {
  const api = useAdminApi();
  const [password, setPassword] = useState("");
  // An admin without MFA gets a new secret at once, for the setup step.
  const signIn = useAction(async () => {
    const result = await api.login({ email, password });
    return needsMfaSetup(result) ? { setup: await api.loginMfaSetup() } : {};
  });
  async function submit(event: FormEvent) {
    event.preventDefault();
    const result = await signIn.run();
    if (!result.ok) return;
    setPassword("");
    onNext(result.value);
  }
  return (
    <form onSubmit={submit} className="grid w-full max-w-sm grid-cols-1 gap-8">
      <Logo href={routes.home} />
      <Heading>Admin sign-in</Heading>
      {restarted && !signIn.error.message && (
        <Notice variant="error">{restarted}</Notice>
      )}
      <ErrorNotice error={signIn.error} />
      <Field disabled={signIn.loading}>
        <Label>Email</Label>
        <Input
          type="email"
          name="email"
          autoComplete="email"
          required
          maxLength={254}
          value={email}
          onChange={(e) => onEmailChange(e.currentTarget.value)}
        />
      </Field>
      <Field disabled={signIn.loading}>
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
      <Button type="submit" className="w-full" loading={signIn.loading}>
        Sign in
      </Button>
    </form>
  );
}
function AdminCodeStep({
  destination,
  onRestart,
}: {
  destination: string;
  onRestart: (reason: string) => void;
}) {
  const api = useAdminApi();
  return (
    <CodeStep
      verify={(code) => api.loginMfa(code)}
      destination={destination}
      onRestart={onRestart}
    />
  );
}
// Setting MFA up at the door: the QR code and the first code (which turns MFA
// on for the whole account and starts the admin session), then the recovery
// codes, which must be saved before going on to Users. The QR code, setup key
// and codes stay hidden until revealed, as in account settings.
function AdminSetup({
  email,
  setup,
  onRestart,
}: {
  email: string;
  setup: MfaSetup;
  onRestart: (reason: string) => void;
}) {
  const api = useAdminApi();
  const key = useAdminKey();
  const navigate = useNavigate();
  const [code, setCode] = useState("");
  const [codeError, setCodeError] = useState<string>();
  const [codes, setCodes] = useState<string[] | null>(null);
  const [saved, setSaved] = useState(false);
  const confirm = useAction(() => api.loginMfa<AdminSetupDone>(code), {
    onError(e) {
      // The challenge or the setup ended: the password is needed again.
      if (e instanceof ApiError && (e.status === 401 || e.status === 409)) {
        onRestart(errorMessage(e));
        return true;
      }
      if (e instanceof ApiError && e.field === "code") {
        setCodeError(errorMessage(e));
        return true;
      }
    },
  });
  const goOn = useAction(async () => {
    await navigate(adminRoutes.users.index(key));
  });
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (codes) {
      await goOn.run();
      return;
    }
    setCodeError(undefined);
    const result = await confirm.run();
    if (result.ok) setCodes(result.value.recoveryCodes);
  }
  return (
    <form onSubmit={submit} className="grid w-full max-w-sm grid-cols-1 gap-8">
      <Logo href={routes.home} />
      <Heading>
        {codes
          ? "Save your recovery codes"
          : "Set up two-factor authentication"}
      </Heading>
      {codes ? (
        <RecoveryCodeList
          codes={codes}
          account={email}
          onSaved={() => setSaved(true)}
        />
      ) : (
        <>
          <Text>
            Admins need two-factor authentication. Scan this with your
            authenticator app.
          </Text>
          <ErrorNotice error={confirm.error} />
          <MfaScanStep
            setup={setup}
            code={code}
            onCodeChange={setCode}
            error={codeError}
            disabled={confirm.loading}
          />
        </>
      )}
      <Button
        type="submit"
        className="w-full"
        loading={codes ? goOn.loading : confirm.loading}
        disabled={codes !== null && !saved}
      >
        {codes ? "I’ve saved these codes" : "Verify"}
      </Button>
    </form>
  );
}
