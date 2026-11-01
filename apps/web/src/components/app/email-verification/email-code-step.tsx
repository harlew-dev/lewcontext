import { useState, type ReactNode } from "react";
import { CodeField } from "@/components/app/mfa/mfa-setup";
import { Button } from "@/components/ui/button";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import { Heading, HeadingGroup } from "@/components/ui/heading";
import { Strong, Text, TextButton } from "@/components/ui/text";
import { ApiError, errorMessage } from "@/lib/api";
import { useAction } from "@/lib/use-action";
import { useAuthSubmit } from "@/lib/use-auth-submit";
// The step of a sign-in page that waits for a code emailed to `email`,
// shared by a sign-up's code page and the signed-in verify-email page: the
// code (six boxes, filled from Mail or Messages on a phone, submitted at the
// sixth digit), Verify, and Send a new code. `verify` sends the code; then it
// opens `destination`, or ?next= or /app (useAuthSubmit). A wrong code shows
// under the field. When the code has ended (a 401 for a sign-up, a 410 for
// an account's code), `onEnded` gets the API's reason; `onConflict` handles a
// 409 and returns true. `children` are the links below.
export function EmailCodeStep({
  email,
  heading,
  verify,
  resend,
  destination,
  onEnded,
  onConflict,
  children,
}: {
  email: string;
  heading: string;
  verify: (code: string) => Promise<unknown>;
  resend: () => Promise<unknown>;
  destination?: string;
  onEnded: (reason: string) => void;
  onConflict?: (error: ApiError) => boolean;
  children?: ReactNode;
}) {
  const [code, setCode] = useState("");
  const [fieldError, setFieldError] = useState<string>();
  const [resent, setResent] = useState(false);
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
  const sendAgain = useAction(resend, {
    onError: (e) => {
      if (e instanceof ApiError && (e.status === 401 || e.status === 410)) {
        onEnded(errorMessage(e));
        return true;
      }
    },
  });
  async function requestCode() {
    setResent(false);
    const result = await sendAgain.run();
    if (!result.ok) return;
    setCode("");
    setFieldError(undefined);
    setResent(true);
  }
  const busy = loading || sendAgain.loading;
  return (
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
          for 15 minutes. If it hasn’t arrived, check your spam folder.
        </Text>
      </HeadingGroup>
      {resent && !error.message && (
        <Notice variant="success">We sent a new code to {email}.</Notice>
      )}
      <ErrorNotice error={error} />
      <ErrorNotice error={sendAgain.error} />
      <CodeField
        label="Verification code"
        value={code}
        onChange={setCode}
        error={fieldError}
        disabled={busy}
        autoSubmit
        autoFocus
      />
      <Button type="submit" className="w-full" loading={loading}>
        Verify
      </Button>
      <div className="space-y-2">
        <Text>
          <TextButton onClick={requestCode} disabled={busy}>
            <Strong>Send a new code</Strong>
          </TextButton>
        </Text>
        {children}
      </div>
    </form>
  );
}
