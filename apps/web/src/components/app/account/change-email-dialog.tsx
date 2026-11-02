import { AppOrRecoveryCode, CodeField } from "@/components/app/mfa/mfa-setup";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogTitle,
} from "@/components/ui/dialog";
import { ErrorNotice, ListSkeleton, Notice } from "@/components/ui/feedback";
import { ErrorMessage, Field, Label } from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";
import { Strong, Text, TextButton } from "@/components/ui/text";
import type { PendingEmailChange } from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import { useOpenCount } from "@/lib/use-open-count";
import { PasswordField } from "./password-fields";
import {
  useChangeEmail,
  type ChangeEmailProps,
  type ChangeEmailState,
} from "./use-change-email";

// Every opening starts with empty factors and reloads the server's pending
// request. Settings, onboarding and the verification gate share this dialog.
export function ChangeEmailDialog(props: ChangeEmailProps) {
  const opened = useOpenCount(props.open);
  return <ChangeEmailForm key={opened} {...props} />;
}

function ChangeEmailForm(props: ChangeEmailProps) {
  const change = useChangeEmail(props);
  return (
    <Dialog open={props.open} onClose={change.close}>
      <form onSubmit={change.submit}>
        <DialogTitle>
          {change.view === "done"
            ? "Email address changed"
            : change.view === "cancelled"
              ? "Your change has been cancelled"
              : "Change email address"}
        </DialogTitle>
        <DialogBody className="space-y-6">
          {!change.ready && !change.status.error && (
            <ListSkeleton rows={2} label="Checking email change" />
          )}
          {change.status.error && !change.fresh && (
            <Notice variant="error" onRetry={change.status.refresh}>
              {change.status.error}
            </Notice>
          )}
          {change.message && (
            <Notice variant="error">{change.message.text}</Notice>
          )}
          <ErrorNotice error={change.request.error} />
          <ErrorNotice error={change.verify.error} />
          <ErrorNotice error={change.cancel.error} />
          {change.ready && <ChangeEmailStep change={change} />}
        </DialogBody>
        <ChangeEmailActions change={change} />
      </form>
    </Dialog>
  );
}

function ChangeEmailStep({ change: c }: { change: ChangeEmailState }) {
  if (c.view === "cancelled")
    return <Text>Your email address was not changed.</Text>;
  if (c.view === "done")
    return (
      <div className="space-y-3">
        <Notice variant="success">
          Your sign-in email is now <Strong>{c.changedTo}</Strong>.
        </Notice>
        <Text>
          Other sessions were signed out. Use the new address the next time you
          sign in. Your previous address receives a notice when email is
          configured.
        </Text>
      </div>
    );
  if (c.view === "destination")
    return (
      <Field disabled={c.loading}>
        <Label>New email address</Label>
        <Input
          type="email"
          name="email"
          autoComplete="email"
          required
          maxLength={254}
          value={c.email}
          invalid={Boolean(c.fields.email)}
          onChange={(e) => c.setEmail(e.currentTarget.value)}
        />
        {c.fields.email && (
          <ErrorMessage role="alert">{c.fields.email}</ErrorMessage>
        )}
      </Field>
    );
  if (c.view === "proof")
    return (
      <div className="space-y-6">
        <Text>
          New sign-in address: <Strong>{c.email}</Strong>
        </Text>
        {c.needsMfa ? (
          <AppOrRecoveryCode
            value={c.factor}
            onChange={c.setFactor}
            error={c.fields.code}
            disabled={c.loading}
            autoFocus
          />
        ) : (
          <PasswordField
            label="Current password"
            autoComplete="current-password"
            value={c.password}
            onChange={c.setPassword}
            error={c.fields.currentPassword}
            disabled={c.loading}
          />
        )}
        {c.verification && (
          <Text>We’ll send a code if this address is available.</Text>
        )}
      </div>
    );
  return (
    <div className="space-y-6">
      {c.pending && <PendingDestination pending={c.pending} />}
      <Text>
        If this address is available and can receive our email, a code will
        arrive. Check spam. If you previously blocked our email, the block link
        in that email lets you unblock this address.
      </Text>
      <CodeField
        label="Email change code"
        value={c.code}
        onChange={c.setCode}
        error={c.fields.code}
        disabled={c.loading}
        autoFocus
        autoSubmit
      />
      <Text>
        <TextButton onClick={c.resend} disabled={c.busy}>
          Send a new code
        </TextButton>{" "}
        requires fresh account confirmation.
      </Text>
      <Text>Closing keeps this request. Cancel change deletes it.</Text>
    </div>
  );
}

function ChangeEmailActions({ change: c }: { change: ChangeEmailState }) {
  if (!c.ready)
    return (
      <DialogActions>
        <Button plain onClick={c.close}>
          Close
        </Button>
      </DialogActions>
    );
  if (c.view === "cancelled")
    return (
      <DialogActions>
        <Button onClick={c.close}>Close</Button>
      </DialogActions>
    );
  if (c.view === "done")
    return (
      <DialogActions>
        <Button loading={c.finish.loading} disabled={c.busy} onClick={c.close}>
          Done
        </Button>
      </DialogActions>
    );
  const label =
    c.view === "destination"
      ? "Continue"
      : c.view === "proof" && c.verification
        ? "Send code"
        : "Change email address";
  return (
    <DialogActions>
      <Button plain onClick={c.close} disabled={c.busy}>
        {c.view === "code" ? "Close" : "Cancel"}
      </Button>
      {c.view === "code" && (
        <Button
          outline
          onClick={c.cancelChange}
          loading={c.cancel.loading}
          disabled={c.request.busy || c.verify.busy}
        >
          Cancel change
        </Button>
      )}
      <Button
        type="submit"
        loading={c.request.loading || c.verify.loading}
        disabled={c.cancel.busy || !c.filled}
        disabledReason="Fill in required fields"
      >
        {label}
      </Button>
    </DialogActions>
  );
}

function PendingDestination({ pending }: { pending: PendingEmailChange }) {
  return (
    <Text>
      Requested address: <Strong>{pending.email}</Strong>. Expires{" "}
      {formatDateTime(pending.expiresAt)}.
    </Text>
  );
}
