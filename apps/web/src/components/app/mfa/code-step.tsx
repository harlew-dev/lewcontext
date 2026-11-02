import { useState } from "react";
import { BackButton } from "@/components/layout/auth-layout";
import { Button } from "@/components/ui/button";
import { ErrorNotice } from "@/components/ui/feedback";
import { Heading, HeadingGroup } from "@/components/ui/heading";
import { Logo } from "@/components/ui/logo";
import { Strong, Text, TextButton } from "@/components/ui/text";
import { ApiError, errorMessage } from "@/lib/api";
import { routes } from "@/lib/routes";
import { useAuthSubmit } from "@/lib/use-auth-submit";
import { CodeField } from "./mfa-setup";
// The second step of a sign-in with two-factor authentication on, shared by
// login and the admin door: a code from the authenticator app, or a recovery
// code, sent with `verify`. Then it opens `destination`, or ?next= or /app
// (useAuthSubmit). The challenge lives in a cookie, so a refresh starts over
// at the password. A 401 means it ended, and a suspended account's right
// code ends it too (403 suspended): back to the password with the API's
// reason.
export function CodeStep({
  verify,
  destination,
  onRestart,
}: {
  verify: (code: string) => Promise<unknown>;
  destination?: string;
  onRestart: (reason: string) => void;
}) {
  const [recovery, setRecovery] = useState(false);
  const [code, setCode] = useState("");
  const [fieldError, setFieldError] = useState<string>();
  const { busy, loading, error, submit } = useAuthSubmit(
    verify,
    destination,
    (e) => {
      if (
        e instanceof ApiError &&
        (e.status === 401 || (e.status === 403 && e.details.suspended === true))
      ) {
        onRestart(errorMessage(e));
        return true;
      }
      if (e instanceof ApiError && e.field === "code") {
        setFieldError(errorMessage(e));
        return true;
      }
    },
  );
  function switchMode() {
    setRecovery(!recovery);
    setCode("");
    setFieldError(undefined);
  }
  return (
    <form
      onSubmit={(event) => {
        setFieldError(undefined);
        return submit(event, code);
      }}
      className="grid w-full max-w-sm grid-cols-1 gap-8"
    >
      <Logo href={routes.home} />
      {/* Back to the password, as whoever is signing in: off from the
          moment a code is sent, not only once loading shows. */}
      <BackButton onClick={() => onRestart("")} disabled={busy} />
      <HeadingGroup>
        <Heading>Two-factor authentication</Heading>
        <Text>
          {recovery
            ? "Enter one of your recovery codes. Each one works once."
            : "Enter the code from your authenticator app."}
        </Text>
      </HeadingGroup>
      <ErrorNotice error={error} />
      <CodeField
        key={recovery ? "recovery" : "app"}
        label={recovery ? "Recovery code" : "Authentication code"}
        value={code}
        onChange={setCode}
        error={fieldError}
        disabled={loading}
        recovery={recovery}
        autoSubmit
        autoFocus
      />
      <Button type="submit" className="w-full" loading={loading}>
        Verify
      </Button>
      <Text>
        <TextButton onClick={switchMode} disabled={loading}>
          <Strong>
            {recovery
              ? "Use your authenticator app instead"
              : "Lost your device? Use a recovery code"}
          </Strong>
        </TextButton>
      </Text>
    </form>
  );
}
