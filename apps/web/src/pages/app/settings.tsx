import { useState, type FormEvent } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Divider } from "@/components/ui/divider";
import { Notice } from "@/components/ui/feedback";
import {
  Description,
  ErrorMessage,
  Field,
  FieldGroup,
  Label,
} from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { SettingsSection } from "@/components/ui/settings-section";
import { Text } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { ApiError, errorMessage, useApi } from "@/lib/api";
import { useRenewSession, useSession } from "@/lib/session";
import { useActionError } from "@/lib/use-action-error";
export default function AccountSettings() {
  const session = useSession();
  return (
    <div className="mx-auto max-w-4xl">
      <Heading>Account & security</Heading>
      <Divider className="my-10 mt-6" />
      <SettingsSection
        title="Profile"
        description="The name and email address on your account."
      >
        <DescriptionList>
          <DescriptionTerm>Name</DescriptionTerm>
          <DescriptionDetails>{session.user.name}</DescriptionDetails>
          <DescriptionTerm>Email address</DescriptionTerm>
          <DescriptionDetails>{session.user.email}</DescriptionDetails>
          <DescriptionTerm>Member since</DescriptionTerm>
          <DescriptionDetails>
            {new Date(session.user.createdAt).toLocaleDateString("en-US", {
              dateStyle: "long",
              timeZone: "UTC",
            })}
          </DescriptionDetails>
        </DescriptionList>
      </SettingsSection>
      <Divider className="my-10" soft />
      <SettingsSection
        title="Password"
        description="Change your account password."
      >
        <PasswordControls />
      </SettingsSection>
      <Divider className="my-10" soft />
      <SettingsSection
        title="Sign-in sessions"
        description="You’re signed out automatically after 30 minutes of inactivity."
      >
        <SessionControls />
      </SettingsSection>
    </div>
  );
}
function PasswordControls() {
  const [open, setOpen] = useState(false);
  // Each opening starts a fresh dialog at its first step.
  const [opened, setOpened] = useState(0);
  return (
    <div>
      <Button
        outline
        onClick={() => {
          setOpened(opened + 1);
          setOpen(true);
        }}
      >
        Change password
      </Button>
      <ChangePasswordDialog
        key={opened}
        open={open}
        onClose={() => setOpen(false)}
      />
    </div>
  );
}
const minimumLength = 12;
// Two steps: the current password, checked by the API before moving on, then
// the new one. Saving sends the current password again, since the API checks
// it on every change. The passwords live only in this dialog's state and are
// cleared when it closes.
function ChangePasswordDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const api = useApi();
  const session = useSession();
  const renew = useRenewSession();
  const [step, setStep] = useState<"current" | "new">("current");
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const error = useActionError();
  function close() {
    if (busy) return;
    setCurrentPassword("");
    setNewPassword("");
    setConfirmation("");
    onClose();
  }
  // Shows an API error next to its field, or above the form.
  function failed(e: unknown) {
    const field = e instanceof ApiError ? e.field : undefined;
    if (field === "password" || field === "currentPassword") {
      error.clear();
      setStep("current");
      setFieldErrors({ currentPassword: errorMessage(e) });
    } else if (field === "newPassword") {
      error.clear();
      setFieldErrors({ newPassword: errorMessage(e) });
    } else {
      error.fail(e);
    }
  }
  async function verify(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    try {
      await api.auth().verifyPassword(currentPassword);
      error.clear();
      setFieldErrors({});
      setStep("new");
    } catch (e) {
      failed(e);
    } finally {
      setBusy(false);
    }
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    const problems = newPasswordProblems(newPassword, confirmation);
    setFieldErrors(problems);
    if (Object.keys(problems).length > 0) return;
    setBusy(true);
    try {
      renew(await api.auth().changePassword({ currentPassword, newPassword }));
      setBusy(false);
      toast.success("Password updated. Other sessions were signed out.");
      setCurrentPassword("");
      setNewPassword("");
      setConfirmation("");
      onClose();
    } catch (e) {
      failed(e);
      setBusy(false);
    }
  }
  return (
    <Dialog open={open} onClose={close}>
      <form onSubmit={step === "current" ? verify : save}>
        <DialogTitle>Change password</DialogTitle>
        <DialogDescription>
          {step === "current"
            ? "Enter your current password to continue."
            : "Choose a new password. Other sessions will be signed out."}
        </DialogDescription>
        <DialogBody>
          {error.message && (
            <Notice key={error.key} variant="error" className="mb-6">
              {error.message}
            </Notice>
          )}
          {/* Tells password managers which account the passwords belong to. */}
          <input
            type="email"
            name="username"
            autoComplete="username"
            value={session.user.email}
            readOnly
            hidden
          />
          {step === "current" ? (
            <PasswordField
              label="Current password"
              autoComplete="current-password"
              value={currentPassword}
              onChange={setCurrentPassword}
              error={fieldErrors.currentPassword}
              disabled={busy}
            />
          ) : (
            <FieldGroup>
              <PasswordField
                label="New password"
                description={`At least ${minimumLength} characters.`}
                autoComplete="new-password"
                value={newPassword}
                onChange={setNewPassword}
                error={fieldErrors.newPassword}
                disabled={busy}
              />
              <PasswordField
                label="Confirm new password"
                autoComplete="new-password"
                value={confirmation}
                onChange={setConfirmation}
                error={fieldErrors.confirmation}
                disabled={busy}
                autoFocus={false}
              />
            </FieldGroup>
          )}
        </DialogBody>
        <DialogActions>
          {step === "current" ? (
            <Button plain onClick={close} disabled={busy}>
              Cancel
            </Button>
          ) : (
            <Button
              plain
              disabled={busy}
              onClick={() => {
                error.clear();
                setFieldErrors({});
                setStep("current");
              }}
            >
              Back
            </Button>
          )}
          <Button type="submit" loading={busy}>
            {step === "current" ? "Next" : "Confirm"}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
// Checks the browser can make before sending a new password.
function newPasswordProblems(
  password: string,
  confirmation: string,
): Record<string, string> {
  if ([...password].length < minimumLength)
    return { newPassword: `Use at least ${minimumLength} characters.` };
  if (password !== confirmation)
    return { confirmation: "The passwords don’t match." };
  return {};
}
function PasswordField({
  label,
  description,
  autoComplete,
  value,
  onChange,
  error,
  disabled,
  autoFocus = true,
}: {
  label: string;
  description?: string;
  autoComplete: string;
  value: string;
  onChange: (value: string) => void;
  error?: string;
  disabled: boolean;
  autoFocus?: boolean;
}) {
  return (
    <Field disabled={disabled}>
      <Label>{label}</Label>
      {description && <Description>{description}</Description>}
      <Input
        type="password"
        autoComplete={autoComplete}
        autoFocus={autoFocus}
        required
        maxLength={128}
        invalid={Boolean(error)}
        value={value}
        onChange={(e) => onChange(e.currentTarget.value)}
      />
      {error && <ErrorMessage role="alert">{error}</ErrorMessage>}
    </Field>
  );
}
function SessionControls() {
  const api = useApi();
  const session = useSession();
  const [busy, setBusy] = useState(false);
  async function revokeOthers() {
    setBusy(true);
    try {
      await api.sessions().destroyOthers();
      toast.success("All other sessions have been signed out.");
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <div className="text-base/6 font-medium text-zinc-950 sm:text-sm/6 dark:text-white">
            This browser
          </div>
          <Text>
            Signed in{" "}
            {new Date(session.signedInAt).toLocaleString("en-US", {
              dateStyle: "medium",
              timeStyle: "short",
              timeZone: "UTC",
            })}{" "}
            UTC
          </Text>
        </div>
        <Badge color="purple">Current session</Badge>
      </div>
      <Button outline loading={busy} onClick={revokeOthers}>
        Sign out other sessions
      </Button>
    </div>
  );
}
