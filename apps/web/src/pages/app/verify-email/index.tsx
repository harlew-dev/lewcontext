import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { EmailCodeStep } from "@/components/app/email-verification/email-code-step";
import { AuthLayout } from "@/components/layout/auth-layout";
import { ServiceUnavailable } from "@/components/layout/status-page";
import { Button } from "@/components/ui/button";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import { Heading, HeadingGroup } from "@/components/ui/heading";
import { Logo } from "@/components/ui/logo";
import { Strong, Text, TextButton } from "@/components/ui/text";
import { errorMessage, useApi } from "@/lib/api";
import { verifyEmailLoader } from "@/lib/loaders";
import { returnPath, routes } from "@/lib/routes";
import { SessionProvider, useSession } from "@/lib/session";
import { useAction } from "@/lib/use-action";
import { useClearCache } from "@/lib/use-clear-cache";
import type { Route } from "./+types/index";
// An account that never verified its email, while the server requires it
// (EMAIL_VERIFICATION_EXISTING=required): every signed-in page sends it here
// first, and the API refuses it everything else. Outside the app layout, like
// onboarding, since the layout's own requests would be refused.
export function clientLoader({ request }: Route.ClientLoaderArgs) {
  return verifyEmailLoader(request);
}
export default function VerifyEmail({ loaderData }: Route.ComponentProps) {
  if ("unavailable" in loaderData)
    return <ServiceUnavailable requestId={loaderData.requestId} />;
  return (
    <SessionProvider session={loaderData.session}>
      <AuthLayout>
        <div className="grid w-full max-w-sm grid-cols-1 gap-8">
          <Logo />
          <VerifySteps />
        </div>
      </AuthLayout>
    </SessionProvider>
  );
}
// Nothing is sent until asked for, so turning the requirement on never
// emails anyone by itself.
function VerifySteps() {
  const api = useApi();
  const session = useSession();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const [step, setStep] = useState<"send" | "code">("send");
  const [ended, setEnded] = useState("");
  const send = useAction(() => api.auth().emailVerification().store());
  const email = session.user.email;
  async function sendCode() {
    setEnded("");
    if ((await send.run()).ok) setStep("code");
  }
  if (step === "code")
    return (
      <EmailCodeStep
        email={email}
        heading="Verify your email"
        verify={(code) => api.auth().emailVerification().update(code)}
        resend={() => api.auth().emailVerification().store()}
        onEnded={(reason) => {
          setEnded(reason);
          setStep("send");
        }}
        // Verified meanwhile (another tab): go on.
        onConflict={() => {
          void navigate(returnPath(search.get("next")) ?? routes.app);
          return true;
        }}
      >
        <SignOut />
      </EmailCodeStep>
    );
  return (
    <div className="grid grid-cols-1 gap-8">
      <HeadingGroup>
        <Heading>Verify your email</Heading>
        <Text>
          You must verify your email to continue. We’ll send a 6-digit code to{" "}
          <Strong>{email}</Strong>.
        </Text>
      </HeadingGroup>
      {ended && <Notice variant="error">{ended}</Notice>}
      <ErrorNotice error={send.error} />
      <Button className="w-full" loading={send.loading} onClick={sendCode}>
        Send code
      </Button>
      <SignOut />
    </div>
  );
}
// The way out for the wrong account, or an address that can't get mail.
function SignOut() {
  const api = useApi();
  const navigate = useNavigate();
  const clearCache = useClearCache();
  const [failed, setFailed] = useState("");
  const signOut = useAction(
    async () => {
      await api.auth().logout();
      await navigate(routes.login);
      clearCache();
    },
    {
      onError: (e) => {
        setFailed(errorMessage(e));
        return true;
      },
    },
  );
  return (
    <div className="space-y-2">
      <Text>
        Wrong account?{" "}
        <TextButton onClick={() => void signOut.run()} disabled={signOut.busy}>
          <Strong>Sign out</Strong>
        </TextButton>
      </Text>
      {failed && <Notice variant="error">{failed}</Notice>}
    </div>
  );
}
