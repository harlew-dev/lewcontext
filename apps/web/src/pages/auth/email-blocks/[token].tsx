import { useEffect, useState } from "react";
import { useParams } from "react-router";
import { AuthLayout } from "@/components/layout/auth-layout";
import { Button } from "@/components/ui/button";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import { Heading, HeadingGroup } from "@/components/ui/heading";
import { Logo } from "@/components/ui/logo";
import { Strong, Text } from "@/components/ui/text";
import { ApiError, errorMessage, useApi } from "@/lib/api";
import { appName } from "@/lib/app";
import { routes } from "@/lib/routes";
import { useAction } from "@/lib/use-action";
import { useDelayedLoading } from "@/lib/use-delayed-loading";
import type { EmailBlockLink } from "@/types/email-blocks";
// The page a block link opens: /auth/email-blocks/<token>, from the footer of
// an invitation (plans/features/implemented/email-blocks.md). It checks the link, then
// offers to block the address it went to, or to unblock it. An address with
// an account can't be blocked: it would lose password reset.
//
// It works signed in or out (no guestLoader), like the reset link. The token
// never leaves this page except in the JSON bodies of its requests.
type LinkState =
  | { kind: "checking" }
  | { kind: "live"; link: EmailBlockLink }
  | { kind: "dead" }
  | { kind: "unreachable"; message: string };
// The shape auth.Token makes in Go: 32 random bytes as 43 base64url characters.
const tokenShape = /^[A-Za-z0-9_-]{43}$/;
// A link the API refused: unknown or expired.
function deadLink(error: unknown): boolean {
  return (
    error instanceof ApiError && error.status === 422 && error.field === "token"
  );
}
export default function EmailBlockShow() {
  const token = useParams().token ?? "";
  const { state, retry, update, markDead } = useLinkCheck(token);
  const checking = useDelayedLoading(state.kind === "checking");
  return (
    <AuthLayout>
      <div className="grid w-full max-w-sm grid-cols-1 gap-8">
        <Logo href={routes.home} />
        {state.kind === "checking" &&
          (checking ? <Text role="status">Checking your link…</Text> : null)}
        {state.kind === "live" &&
          (state.link.registered ? (
            <Registered email={state.link.email} />
          ) : (
            <BlockForm
              token={token}
              link={state.link}
              onChange={update}
              onDead={markDead}
            />
          ))}
        {state.kind === "dead" && <DeadLink />}
        {state.kind === "unreachable" && (
          <>
            <Heading>Block this address</Heading>
            <Notice variant="error" onRetry={retry}>
              {state.message}
            </Notice>
          </>
        )}
      </div>
    </AuthLayout>
  );
}
// Asks the API what the link shows, again on retry.
function useLinkCheck(token: string) {
  const api = useApi();
  const wellFormed = tokenShape.test(token);
  const [state, setState] = useState<LinkState>(
    wellFormed ? { kind: "checking" } : { kind: "dead" },
  );
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!wellFormed) return;
    let current = true;
    api
      .emailBlocks()
      .check(token)
      .then(
        (link) => current && setState({ kind: "live", link }),
        (error: unknown) =>
          current &&
          setState(
            deadLink(error)
              ? { kind: "dead" }
              : { kind: "unreachable", message: errorMessage(error) },
          ),
      );
    return () => {
      current = false;
    };
  }, [api, token, wellFormed, attempt]);
  return {
    state,
    retry: () => {
      setState({ kind: "checking" });
      setAttempt(attempt + 1);
    },
    update: (link: EmailBlockLink) => setState({ kind: "live", link }),
    markDead: () => setState({ kind: "dead" }),
  };
}
// Blocks the address, or unblocks it, and switches to the other state with
// a status message. A failure keeps the page as it was, with the error.
function BlockForm({
  token,
  link,
  onChange,
  onDead,
}: {
  token: string;
  link: EmailBlockLink;
  onChange: (link: EmailBlockLink) => void;
  onDead: () => void;
}) {
  const api = useApi();
  const [done, setDone] = useState<string | null>(null);
  const change = useAction(
    async () => {
      const blocks = api.emailBlocks();
      await (link.blocked ? blocks.destroy(token) : blocks.store(token));
      setDone(link.blocked ? "Address unblocked." : "Address blocked.");
      onChange({ ...link, blocked: !link.blocked });
    },
    {
      // A link that expired meanwhile shows the expired page; an account
      // made with the address meanwhile, the account's state.
      onError: (e) => {
        if (deadLink(e)) {
          onDead();
          return true;
        }
        if (e instanceof ApiError && e.status === 409) {
          onChange({ ...link, registered: true });
          return true;
        }
        return false;
      },
    },
  );
  return (
    <div className="grid grid-cols-1 gap-8">
      {link.blocked ? (
        <HeadingGroup>
          <Heading>This address is blocked</Heading>
          <Text>
            <Strong>{link.email}</Strong> is blocked. You won’t receive email
            from {appName}, and it can’t be used to sign up. Keep this email:
            its link lets you unblock this address for a year.
          </Text>
        </HeadingGroup>
      ) : (
        <HeadingGroup>
          <Heading>Block this address?</Heading>
          <Text>
            Block <Strong>{link.email}</Strong>? You won’t receive any more
            email from {appName}, and this address can’t be used to sign up. You
            can undo this from this link for a year.
          </Text>
        </HeadingGroup>
      )}
      {done && <Notice variant="success">{done}</Notice>}
      <ErrorNotice error={change.error} />
      <Button
        className="w-full"
        loading={change.loading}
        onClick={() => {
          setDone(null);
          void change.run();
        }}
      >
        {link.blocked ? "Unblock" : "Block this address"}
      </Button>
    </div>
  );
}
function Registered({ email }: { email: string }) {
  return (
    <>
      <HeadingGroup>
        <Heading>This address has an account</Heading>
        <Text>
          <Strong>{email}</Strong> has a {appName} account. To stop email from{" "}
          {appName}, delete your account first: sign in, then Account &amp;
          security.
        </Text>
      </HeadingGroup>
      <Button href={routes.login} className="w-full">
        Sign in
      </Button>
    </>
  );
}
function DeadLink() {
  return (
    <>
      <Heading>This link has expired</Heading>
      <Notice variant="error">This link has expired or isn’t valid.</Notice>
    </>
  );
}
