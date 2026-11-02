import { useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";
import { EmailCodeStep } from "@/components/app/email-verification/email-code-step";
import { AuthLayout, BackButton } from "@/components/layout/auth-layout";
import { ServiceUnavailable } from "@/components/layout/status-page";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { Logo } from "@/components/ui/logo";
import { Strong, Text, TextLink } from "@/components/ui/text";
import { useApi } from "@/lib/api";
import { registerVerifyLoader } from "@/lib/loaders";
import {
  accountExistsLoginPath,
  authPageState,
  authPath,
  invitationToken,
  routes,
} from "@/lib/routes";
import type { Route } from "./+types/index";
// A sign-up waiting for the code sent to its email (email verification on).
// The sign-up lives in a cookie, so a reload, such as a phone switching to the
// mail app and back, lands here again; the loader asks the API which email
// it's for. Signed-in visitors go on, as from register.
export function clientLoader({ request }: Route.ClientLoaderArgs) {
  return registerVerifyLoader(request);
}
export default function RegisterVerify({ loaderData }: Route.ComponentProps) {
  const [ended, setEnded] = useState("");
  if ("unavailable" in loaderData)
    return <ServiceUnavailable requestId={loaderData.requestId} />;
  if ("ended" in loaderData || ended)
    return (
      <SignUpEnded
        reason={ended || ("ended" in loaderData ? loaderData.ended : "")}
      />
    );
  return <CodeStep email={loaderData.pending.email} onEnded={setEnded} />;
}
function CodeStep({
  email,
  onEnded,
}: {
  email: string;
  onEnded: (reason: string) => void;
}) {
  const api = useApi();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const next = search.get("next");
  const { invitation } = authPageState(useLocation().state);
  const state = invitation ? { invitation: true } : undefined;
  return (
    <AuthLayout>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <Logo href={routes.home} />
        {/* Back to the form, with the email in it, to use another. */}
        <BackButton
          href={authPath(routes.register, next)}
          state={{ ...state, email }}
        />
        <EmailCodeStep
          email={email}
          heading="Check your email"
          verify={(code) => api.auth().registerVerify(code)}
          resend={() => api.auth().registerResend()}
          // A new account starts at onboarding, unless it came to accept an
          // invitation: then it goes back to the link.
          destination={invitationToken(next) ? undefined : routes.onboarding}
          onEnded={onEnded}
          onConflict={() => {
            void navigate(accountExistsLoginPath(next), {
              state: { ...state, email },
            });
            return true;
          }}
          // The same for every address, so it says nothing about which are
          // blocked: only that inbox's owner knows if it means them. An
          // account's email can't be blocked, so only sign-up says it.
          resendHelp={<BlockedHelp />}
        >
          <Text>
            Already have an account?{" "}
            <TextLink href={authPath(routes.login, next)} state={state}>
              <Strong>Sign in</Strong>
            </TextLink>
          </Text>
        </EmailCodeStep>
      </div>
    </AuthLayout>
  );
}
// The sign-up has expired, used up its codes, or was never started here.
function SignUpEnded({ reason }: { reason: string }) {
  const [search] = useSearchParams();
  const { invitation } = authPageState(useLocation().state);
  return (
    <AuthLayout>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <Logo href={routes.home} />
        <Heading>Start again</Heading>
        <Notice variant="error">
          {reason || "This sign-up has expired. Start again."}
        </Notice>
        <Button
          href={authPath(routes.register, search.get("next"))}
          state={invitation ? { invitation: true } : undefined}
          className="w-full"
        >
          Back to sign up
        </Button>
      </div>
    </AuthLayout>
  );
}
// Why no code may come: the address was blocked from one of our emails, and
// how to undo that (the block link's page also unblocks).
function BlockedHelp() {
  return (
    <Text>
      <Strong>Did you block this address?</Strong> If you ever used “Block this
      address” in one of our emails, we can’t send anything to it, codes
      included. Open that email again and follow the same link: its page lets
      you unblock the address. Then send a new code.
    </Text>
  );
}
