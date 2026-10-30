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
import { ErrorNotice } from "@/components/ui/feedback";
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
import { formatDate, formatDateTime } from "@/lib/format";
import { useRenewSession, useSession } from "@/lib/session";
import { useAction } from "@/lib/use-action";
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
            {formatDate(session.user.createdAt, "long")}
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
  // An API error about one field shows under it (a wrong current password
  // also returns to the first step); anything else goes above the form.
  function showOnField(e: unknown): boolean {
    const field = e instanceof ApiError ? e.field : undefined;
    if (field === "password" || field === "currentPassword") {
      setStep("current");
      setFieldErrors({ currentPassword: errorMessage(e) });
      return true;
    }
    if (field === "newPassword") {
      setFieldErrors({ newPassword: errorMessage(e) });
      return true;
    }
    return false;
  }
  const verify = useAction(() => api.auth().verifyPassword(currentPassword), {
    onError: showOnField,
  });
  const change = useAction(
    () => api.auth().changePassword({ currentPassword, newPassword }),
    { onError: showOnField },
  );
  const busy = verify.busy || change.busy;
  const loading = verify.loading || change.loading;
  const error = step === "current" ? verify.error : change.error;
  function clearPasswords() {
    setCurrentPassword("");
    setNewPassword("");
    setConfirmation("");
  }
  function close() {
    if (busy) return;
    clearPasswords();
    onClose();
  }
  async function submitCurrent(event: FormEvent) {
    event.preventDefault();
    if (!(await verify.run()).ok) return;
    change.error.clear();
    setFieldErrors({});
    setStep("new");
  }
  async function submitNew(event: FormEvent) {
    event.preventDefault();
    const problems = newPasswordProblems(newPassword, confirmation);
    setFieldErrors(problems);
    if (Object.keys(problems).length > 0) return;
    const result = await change.run();
    if (!result.ok) return;
    renew(result.value);
    toast.success("Password updated. Other sessions were signed out.");
    clearPasswords();
    onClose();
  }
  function back() {
    if (busy) return;
    verify.error.clear();
    change.error.clear();
    setFieldErrors({});
    setStep("current");
  }
  return (
    <Dialog open={open} onClose={close}>
      <form onSubmit={step === "current" ? submitCurrent : submitNew}>
        <DialogTitle>Change password</DialogTitle>
        <DialogDescription>
          {step === "current"
            ? "Enter your current password to continue."
            : "Choose a new password. Other sessions will be signed out."}
        </DialogDescription>
        <DialogBody>
          <ErrorNotice error={error} className="mb-6" />
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
              disabled={loading}
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
                disabled={loading}
              />
              <PasswordField
                label="Confirm new password"
                autoComplete="new-password"
                value={confirmation}
                onChange={setConfirmation}
                error={fieldErrors.confirmation}
                disabled={loading}
                autoFocus={false}
              />
            </FieldGroup>
          )}
        </DialogBody>
        <DialogActions>
          {step === "current" ? (
            <Button plain onClick={close} disabled={loading}>
              Cancel
            </Button>
          ) : (
            <Button plain disabled={loading} onClick={back}>
              Back
            </Button>
          )}
          <Button type="submit" loading={loading}>
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
  const revoke = useAction(() => api.sessions().destroyOthers(), {
    onError: (e) => {
      toast.error(errorMessage(e));
      return true;
    },
  });
  async function revokeOthers() {
    if ((await revoke.run()).ok)
      toast.success("All other sessions have been signed out.");
  }
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <div className="text-base/6 font-medium text-zinc-950 sm:text-sm/6 dark:text-white">
            This browser
          </div>
          <Text>Signed in {formatDateTime(session.signedInAt)}</Text>
        </div>
        <Badge color="purple">Current session</Badge>
      </div>
      <Button outline loading={revoke.loading} onClick={revokeOthers}>
        Sign out other sessions
      </Button>
    </div>
  );
}
