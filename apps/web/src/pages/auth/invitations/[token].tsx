import { useEffect, useRef } from "react";
import { useLocation, useNavigate, useParams } from "react-router";
import { InvitationCallout } from "@/components/app/invitations/invitation-callout";
import { AuthLayout } from "@/components/layout/auth-layout";
import { Button } from "@/components/ui/button";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { Logo } from "@/components/ui/logo";
import { Strong, Text, TextLink } from "@/components/ui/text";
import { loaderApi, useApi } from "@/lib/api";
import {
  authPageState,
  authPath,
  routes,
  type ArrivalState,
  type AuthPageState,
} from "@/lib/routes";
import { SessionProvider, useOptionalSession } from "@/lib/session";
import type { Session } from "@/lib/types";
import { useAction } from "@/lib/use-action";
import { useClearCache } from "@/lib/use-clear-cache";
import { useInvitation } from "@/lib/use-invitation";
import type { InvitationPreview } from "@/types/invitations";
import type { Route } from "./+types/[token]";
// The page an invitation email links to: /auth/invitations/<token>. It works
// signed in and out (no guestLoader), like the reset link. Signed out, it
// offers sign-in and sign-up, which come back here; signed in as the invited
// email, Join accepts. Joining is always a deliberate step: it happens at once
// only when the visitor arrives from this page's own buttons (navigation
// state, which a crafted link can't set). The token leaves this page only in
// the JSON bodies of check and accept, and in ?next= for the sign-in pages.
export async function clientLoader(): Promise<{ session: Session | null }> {
  const session = await loaderApi()
    .auth()
    .me()
    .catch(() => null);
  return { session };
}
export default function InvitationShow({ loaderData }: Route.ComponentProps) {
  return (
    <SessionProvider session={loaderData.session}>
      <AuthLayout>
        <div className="grid w-full max-w-sm grid-cols-1 gap-8">
          <Logo href={routes.home} />
          <InvitationLink />
        </div>
      </AuthLayout>
    </SessionProvider>
  );
}
function InvitationLink() {
  const token = useParams().token ?? "";
  const check = useInvitation(token);
  const session = useOptionalSession();
  if (check.kind === "dead") return <DeadInvitation />;
  if (check.kind !== "live")
    return (
      <>
        <Heading>Join a workspace</Heading>
        <InvitationCallout check={check} ending="" dead="" />
      </>
    );
  const { invitation } = check;
  return (
    <>
      <Heading>Join {invitation.workspace.name}</Heading>
      {!session ? (
        <SignedOut token={token} invitation={invitation} check={check} />
      ) : session.user.email === invitation.email ? (
        <Join token={token} invitation={invitation} check={check} />
      ) : (
        <WrongAccount
          token={token}
          invitation={invitation}
          signedInAs={session.user.email}
        />
      )}
    </>
  );
}
type InvitationProps = {
  token: string;
  invitation: InvitationPreview;
  check: Parameters<typeof InvitationCallout>[0]["check"];
};
// Sign in, or create an account, as the invited email; either comes back here
// and joins at once.
function SignedOut({ token, invitation, check }: InvitationProps) {
  const here = routes.invitations.show(token);
  const state: AuthPageState = { email: invitation.email, invitation: true };
  return (
    <>
      <InvitationCallout
        check={check}
        ending="Sign in or create an account to accept."
        dead=""
      />
      <Button
        href={authPath(routes.login, here)}
        state={state}
        className="w-full"
      >
        Sign in to accept
      </Button>
      <Text>
        New here?{" "}
        <TextLink href={authPath(routes.register, here)} state={state}>
          <Strong>Create an account</Strong>
        </TextLink>
      </Text>
    </>
  );
}
// Signed in as the invited email: Join, or join at once when arriving from
// signing in or registering through this page.
function Join({ token, invitation, check }: InvitationProps) {
  const api = useApi();
  const navigate = useNavigate();
  const arrived = authPageState(useLocation().state);
  const join = useAction(async () => {
    const workspace = await api.invitations().accept(token);
    const state: ArrivalState = { joinedWorkspace: workspace.name };
    await navigate(routes.dashboard(workspace.id), { replace: true, state });
  });
  // Once, even when React runs effects twice in development (the ref
  // survives the remount).
  const tried = useRef(false);
  useEffect(() => {
    if (!arrived.invitation || tried.current) return;
    tried.current = true;
    void join.run();
  });
  return (
    <>
      <InvitationCallout check={check} ending="Join below to accept." dead="" />
      <ErrorNotice error={join.error} />
      <Button
        className="w-full"
        loading={join.loading}
        onClick={() => void join.run()}
      >
        Join {invitation.workspace.name}
      </Button>
    </>
  );
}
// Signed in as someone else: the invitation only works for its email.
function WrongAccount({
  token,
  invitation,
  signedInAs,
}: {
  token: string;
  invitation: InvitationPreview;
  signedInAs: string;
}) {
  const api = useApi();
  const navigate = useNavigate();
  const clearCache = useClearCache();
  const switchAccount = useAction(async () => {
    await api.auth().logout();
    const state: AuthPageState = { email: invitation.email, invitation: true };
    await navigate(authPath(routes.login, routes.invitations.show(token)), {
      state,
    });
    clearCache();
  });
  return (
    <>
      <Notice variant="error">
        This invitation is for {invitation.email}; you’re signed in as{" "}
        {signedInAs}.
      </Notice>
      <ErrorNotice error={switchAccount.error} />
      <Button
        className="w-full"
        loading={switchAccount.loading}
        onClick={() => void switchAccount.run()}
      >
        Sign out and switch
      </Button>
    </>
  );
}
function DeadInvitation() {
  return (
    <>
      <Heading>This invitation has expired</Heading>
      <Notice variant="error">
        It was revoked, has expired, or has already been used. Ask whoever
        invited you to send a new one.
      </Notice>
      <Text>
        <TextLink href={routes.home}>
          <Strong>Go to the homepage</Strong>
        </TextLink>
      </Text>
    </>
  );
}
