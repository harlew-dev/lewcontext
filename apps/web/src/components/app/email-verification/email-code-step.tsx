import { useState, type ReactNode } from "react";
import { CodeField } from "@/components/app/mfa/mfa-setup";
import { Button } from "@/components/ui/button";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import { Heading, HeadingGroup } from "@/components/ui/heading";
import { Strong, Text, TextButton } from "@/components/ui/text";
import { ApiError, errorMessage } from "@/lib/api";
import { useAuthSubmit } from "@/lib/use-auth-submit";
import { useOpenCount } from "@/lib/use-open-count";
// The step of a sign-in page that waits for a code emailed to `email`,
// shared by a sign-up's code page and the signed-in verify-email page: the
// code (six boxes, filled from Mail or Messages on a phone, submitted at the
// sixth digit), Verify, and "Didn’t receive the email?", a dialog that
// says what to check and sends a new code. `verify` sends the code; then it
// opens `destination`, or ?next= or /app (useAuthSubmit). A wrong code shows
// under the field. When the code has ended (a 401 for a sign-up, a 410 for
// an account's code), `onEnded` gets the API's reason; `onConflict` handles a
// 409 and returns true. `resendHelp` adds to the dialog (the sign-up's word
// on blocked addresses); `children` are the links below.
export function EmailCodeStep({
  email,
  heading,
  verify,
  resend,
  destination,
  onEnded,
  onConflict,
  resendHelp,
  children,
}: {
  email: string;
  heading: string;
  verify: (code: string) => Promise<unknown>;
  resend: () => Promise<unknown>;
  destination?: string;
  onEnded: (reason: string) => void;
  onConflict?: (error: ApiError) => boolean;
  resendHelp?: ReactNode;
  children?: ReactNode;
}) {
  const [code, setCode] = useState("");
  const [fieldError, setFieldError] = useState<string>();
  const [resent, setResent] = useState(false);
  const [asking, setAsking] = useState(false);
  const opened = useOpenCount(asking);
  const { loading, error, submit } = useAuthSubmit(verify, destination, (e) => {
    if (!(e instanceof ApiError)) return;
    if (e.status === 401 || e.status === 410) {
      onEnded(errorMessage(e));
      return true;
    }
    if (e.status === 409 && onConflict) return onConflict(e);
    if (e.field === "code") {
      setFieldError(errorMessage(e));
      return true;
    }
  });
  function sent() {
    setCode("");
    setFieldError(undefined);
    setResent(true);
  }
  return (
    <>
      <form
        onSubmit={(event) => {
          setFieldError(undefined);
          return submit(event, code);
        }}
        className="grid grid-cols-1 gap-8"
      >
        <HeadingGroup>
          <Heading>{heading}</Heading>
          <Text>
            Enter the 6-digit code we sent to <Strong>{email}</Strong>. It works
            for 15 minutes.
          </Text>
        </HeadingGroup>
        {resent && !error.message && (
          <Notice variant="success">We sent a new code to {email}.</Notice>
        )}
        <ErrorNotice error={error} />
        <CodeField
          label="Verification code"
          value={code}
          onChange={setCode}
          error={fieldError}
          disabled={loading}
          autoSubmit
          autoFocus
        />
        <Button type="submit" className="w-full" loading={loading}>
          Verify
        </Button>
        <div className="space-y-2">
          <Text>
            <TextButton
              onClick={() => {
                setResent(false);
                setAsking(true);
              }}
              disabled={loading}
            >
              <Strong>Didn’t receive the email?</Strong>
            </TextButton>
          </Text>
          {children}
        </div>
      </form>
      {/* Outside the form: React passes a submit up through the portal, and
          the dialog's own form would submit the code too. */}
      <ResendDialog
        key={opened}
        open={asking}
        onClose={() => setAsking(false)}
        resend={resend}
        onSent={sent}
        onEnded={onEnded}
      >
        {resendHelp}
      </ResendDialog>
    </>
  );
}
// "Didn’t receive the email?": what to check, then Cancel or Resend code.
// The same for every address, so it never says whether one can get mail.
// A code that has ended (401, 410) ends the step; any other failure, such as
// the rate limit, stays in the dialog.
function ResendDialog({
  open,
  onClose,
  resend,
  onSent,
  onEnded,
  children,
}: {
  open: boolean;
  onClose: () => void;
  resend: () => Promise<unknown>;
  onSent: () => void;
  onEnded: (reason: string) => void;
  children?: ReactNode;
}) {
  return (
    <ConfirmationDialog
      open={open}
      onClose={onClose}
      title="Didn’t receive the email?"
      description="It can take a minute or two to arrive. Check your spam or junk folder, then send a new code if it isn’t there. A new code replaces the one before."
      confirm="Resend code"
      onConfirm={resend}
      onConfirmed={onSent}
      onError={(e) => {
        if (e instanceof ApiError && (e.status === 401 || e.status === 410)) {
          onEnded(errorMessage(e));
          return true;
        }
      }}
    >
      {children}
    </ConfirmationDialog>
  );
}
