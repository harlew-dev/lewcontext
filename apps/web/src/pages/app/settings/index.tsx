import { useState, type FormEvent } from "react";
import {
  ChevronRightIcon,
  EyeIcon,
  EyeSlashIcon,
} from "@heroicons/react/16/solid";
import { DeleteAccountDialog } from "@/components/app/account/delete-account-dialog";
import { ChangeEmailDialog } from "@/components/app/account/change-email-dialog";
import {
  AppOrRecoveryCode,
  CodeField,
  MfaScanStep,
  RecoveryCodeList,
} from "@/components/app/mfa/mfa-setup";
import { WorkspaceAvatar } from "@/components/app/workspaces/workspace-avatar";
import {
  NewPasswordFields,
  newPasswordProblems,
  PasswordField,
} from "@/components/app/account/password-fields";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Divider } from "@/components/ui/divider";
import { ErrorNotice, Notice } from "@/components/ui/feedback";
import {
  Description,
  Field,
  FieldGroup,
  Label,
} from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import {
  Input,
  InputActions,
  InputButton,
  InputGroup,
} from "@/components/ui/input";
import { Link } from "@/components/ui/link";
import {
  DangerAction,
  SettingsSection,
} from "@/components/ui/settings-section";
import { Text, TextButton, TextLink } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { Tooltip } from "@/components/ui/tooltip";
import {
  ApiError,
  errorMessage,
  paths,
  useApi,
  type MfaSetup,
  type WorkspaceSummary,
} from "@/lib/api";
import { formatDate, formatDateTime } from "@/lib/format";
import { routes } from "@/lib/routes";
import { useRenewSession, useSession } from "@/lib/session";
import { useAction } from "@/lib/use-action";
import { useOpenCount } from "@/lib/use-open-count";
import { useResource } from "@/lib/use-resource";
export default function AccountSettings() {
  const session = useSession();
  return (
    <div className="mx-auto max-w-4xl">
      <Heading>Account & security</Heading>
      <Divider className="my-10 mt-6" />
      <SettingsSection title="Profile">
        <ProfileForm key={session.user.id} />
      </SettingsSection>
      <Divider className="my-10" soft />
      <SettingsSection title="Password">
        <PasswordControls />
      </SettingsSection>
      <Divider className="my-10" soft />
      <SettingsSection title="Two-factor authentication">
        <MfaControls />
      </SettingsSection>
      <Divider className="my-10" soft />
      <SettingsSection title="Sign-in sessions">
        <SessionControls />
      </SettingsSection>
      <PoliciesSection />
      <Divider className="my-10" soft />
      <SettingsSection
        title="Your data"
        description="A copy of everything we keep about your account, as a JSON file."
      >
        <DataExportControls />
      </SettingsSection>
      <Divider className="my-10" soft />
      <SettingsSection
        title="Danger zone"
        description="Deleting your account takes effect at once and can’t be undone."
        variant="danger"
      >
        <DeleteAccountControls />
      </SettingsSection>
    </div>
  );
}
const maxName = 80;
// The trimmed name, or null when it's empty or too long for the API.
function cleanName(value: string): string | null {
  const name = value.trim();
  const length = Array.from(name).length;
  return length >= 1 && length <= maxName ? name : null;
}
// Saves the name at once, then renews the session with the updated user so
// the new name shows everywhere. An edited email is never saved by this form:
// Save opens the change-email dialog for it (account access, then the new
// inbox's code), after the name, so a name edited with it isn't lost if that
// is closed. The current address starts blurred in case the screen is shared.
function ProfileForm() {
  const api = useApi();
  const session = useSession();
  const renew = useRenewSession();
  const [name, setName] = useState(session.user.name);
  const [email, setEmail] = useState(session.user.email);
  const [emailVisible, setEmailVisible] = useState(false);
  // With email verification on, the email says whether it's verified, and an
  // unverified one offers to verify it.
  const verification = session.emailVerification !== "off";
  const unverified = verification && !session.user.emailVerifiedAt;
  const [verifying, setVerifying] = useState(false);
  const verifyOpened = useOpenCount(verifying);
  // "new": Save opened it for the typed address. "resume": the description's
  // Continue opened it for the request already sent.
  const [changingEmail, setChangingEmail] = useState<"new" | "resume" | null>(
    null,
  );
  // The same cached request the dialog reads and updates. A failed read just
  // leaves the field without its description.
  // With verification off a change completes at once, so none is ever waiting.
  const emailChange = useResource(
    verification ? paths.auth.emailChange().show() : null,
  );
  const resumable = Boolean(emailChange.data?.pending);
  const save = useAction((clean: string) =>
    api.auth().updateProfile({ name: clean }),
  );
  const clean = cleanName(name);
  const newEmail = email.trim();
  const nameChanged = clean !== null && clean !== session.user.name;
  const emailChanged =
    newEmail !== "" &&
    newEmail.toLowerCase() !== session.user.email.toLowerCase();
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (clean === null || (!nameChanged && !emailChanged)) return;
    if (nameChanged) {
      const result = await save.run(clean);
      if (!result.ok) return;
      renew({ ...session, user: result.value });
      setName(result.value.name);
      toast.success("Profile updated.");
    }
    if (emailChanged) setChangingEmail("new");
  }
  return (
    // The dialog sits outside the form: React passes a dialog form's submit
    // up through its portal to an enclosing form.
    <>
      <form onSubmit={submit} className="space-y-4">
        <FieldGroup>
          <Field disabled={save.loading}>
            <Label>Name</Label>
            <Input
              name="name"
              autoComplete="name"
              required
              maxLength={maxName}
              value={name}
              onChange={(e) => setName(e.currentTarget.value)}
            />
          </Field>
          <Field guarded="Protected">
            <Label>
              Email address
              {verification && <EmailStatusBadge verified={!unverified} />}
            </Label>
            <InputGroup>
              <Input
                type="email"
                name="email"
                autoComplete="email"
                maxLength={254}
                value={email}
                blurred={!emailVisible}
                onReveal={() => setEmailVisible(true)}
                onChange={(e) => {
                  setEmailVisible(true);
                  setEmail(e.currentTarget.value);
                }}
              />
              <InputActions>
                <Tooltip content={emailVisible ? "Hide" : "Reveal"}>
                  <InputButton
                    aria-label={
                      emailVisible
                        ? "Hide email address"
                        : "Reveal email address"
                    }
                    aria-pressed={emailVisible}
                    onClick={() => setEmailVisible(!emailVisible)}
                  >
                    {emailVisible ? <EyeSlashIcon /> : <EyeIcon />}
                  </InputButton>
                </Tooltip>
              </InputActions>
            </InputGroup>
            {(unverified || resumable) && (
              <Description>
                {unverified && (
                  <>
                    We haven’t confirmed this address.{" "}
                    <TextButton onClick={() => setVerifying(true)}>
                      Verify email
                    </TextButton>
                    .{" "}
                  </>
                )}
                {resumable && (
                  <>
                    You have an email change request.{" "}
                    <TextButton
                      onClick={() => setChangingEmail("resume")}
                      disabled={save.busy}
                    >
                      Continue
                    </TextButton>
                  </>
                )}
              </Description>
            )}
          </Field>
        </FieldGroup>
        <div className="flex justify-end">
          <Button
            type="submit"
            loading={save.loading}
            disabled={clean === null || (!nameChanged && !emailChanged)}
            disabledReason={
              clean === null ? "Fill in required fields" : "No changes made"
            }
          >
            Save
          </Button>
        </div>
        <ErrorNotice error={save.error} />
      </form>
      <VerifyEmailDialog
        key={verifyOpened}
        open={verifying}
        onClose={() => setVerifying(false)}
      />
      <ChangeEmailDialog
        open={changingEmail !== null}
        initialEmail={changingEmail === "new" ? newEmail : undefined}
        onClose={() => setChangingEmail(null)}
        onChanged={(changed) => {
          setEmail(changed);
          setEmailVisible(false);
        }}
      />
    </>
  );
}
// Part of the email field's label, so it's read with it.
function EmailStatusBadge({ verified }: { verified: boolean }) {
  return (
    <>
      {" "}
      <Badge color={verified ? "lime" : "zinc"} className="ml-1 align-middle">
        {verified ? "Verified" : "Not verified"}
      </Badge>
    </>
  );
}
// Verifies the account's email with a code sent to it: Send code, then the
// code. The address isn't shown, as the field above keeps it blurred.
function VerifyEmailDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const api = useApi();
  const session = useSession();
  const renew = useRenewSession();
  const [step, setStep] = useState<"send" | "code">("send");
  const [code, setCode] = useState("");
  const [fieldError, setFieldError] = useState<string>();
  const [ended, setEnded] = useState("");
  // A code that ended (expired, or too many wrong ones) goes back to sending
  // a new one, saying why; a wrong one shows under the field.
  function showError(e: unknown): boolean {
    if (e instanceof ApiError && e.status === 410) {
      setEnded(errorMessage(e));
      setCode("");
      setStep("send");
      return true;
    }
    if (e instanceof ApiError && e.field === "code") {
      setFieldError(errorMessage(e));
      return true;
    }
    return false;
  }
  const send = useAction(() => api.auth().emailVerification().store());
  const verify = useAction(
    (typed: string) => api.auth().emailVerification().update(typed),
    { onError: showError },
  );
  const busy = send.busy || verify.busy;
  const loading = send.loading || verify.loading;
  function close() {
    if (!busy) onClose();
  }
  function verified(emailVerifiedAt: string | null) {
    renew({ ...session, user: { ...session.user, emailVerifiedAt } });
    toast.success("Email verified.");
    onClose();
  }
  async function sendCode() {
    setEnded("");
    setFieldError(undefined);
    if (!(await send.run()).ok) return;
    if (step === "code") toast.success("We sent a new code.");
    setCode("");
    setStep("code");
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (step === "send") return sendCode();
    setFieldError(undefined);
    const result = await verify.run(code);
    if (result.ok) verified(result.value.user.emailVerifiedAt);
  }
  return (
    <Dialog open={open} onClose={close}>
      <form onSubmit={submit}>
        <DialogTitle>Verify your email</DialogTitle>
        <DialogDescription>
          {step === "send"
            ? "We’ll send a 6-digit code to your email address."
            : "Enter the 6-digit code we sent to your email address. It works for 15 minutes."}
        </DialogDescription>
        <DialogBody>
          {ended && (
            <Notice variant="error" className="mb-6">
              {ended}
            </Notice>
          )}
          <ErrorNotice
            error={step === "send" ? send.error : verify.error}
            className="mb-6"
          />
          {step === "code" && (
            <div className="space-y-4">
              <CodeField
                label="Verification code"
                value={code}
                onChange={setCode}
                error={fieldError}
                disabled={loading}
                autoSubmit
                autoFocus
              />
              <Text>
                <TextButton onClick={sendCode} disabled={busy}>
                  Send a new code
                </TextButton>
              </Text>
            </div>
          )}
        </DialogBody>
        <DialogActions>
          <Button plain onClick={close} disabled={loading}>
            Cancel
          </Button>
          <Button type="submit" loading={loading}>
            {step === "send" ? "Send code" : "Verify"}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
function PasswordControls() {
  const [open, setOpen] = useState(false);
  // Each opening starts a fresh dialog at its first step.
  const [opened, setOpened] = useState(0);
  return (
    <div className="flex items-start justify-end">
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
// Two steps. Without MFA: the current password, checked by the API before
// moving on, then the new one; saving sends the current password again, since
// the API checks it on every change. With MFA on: the new password, then a
// code from the authenticator app (or a recovery code), which the API takes
// instead of the current password. What's typed lives only in this dialog's
// state and is cleared when it closes.
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
  const mfa = session.user.mfaEnabledAt !== null;
  const [step, setStep] = useState<"current" | "new" | "code">(
    mfa ? "new" : "current",
  );
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [code, setCode] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  // An API error about one field shows under it, on that field's step;
  // anything else goes above the form.
  function showOnField(e: unknown): boolean {
    const field = e instanceof ApiError ? e.field : undefined;
    if (field === "password" || field === "currentPassword") {
      setStep("current");
      setFieldErrors({ currentPassword: errorMessage(e) });
      return true;
    }
    if (field === "newPassword") {
      setStep("new");
      setFieldErrors({ newPassword: errorMessage(e) });
      return true;
    }
    if (field === "code") {
      setFieldErrors({ code: errorMessage(e) });
      return true;
    }
    return false;
  }
  const verify = useAction(() => api.auth().verifyPassword(currentPassword), {
    onError: showOnField,
  });
  const change = useAction(
    () =>
      api
        .auth()
        .changePassword(
          mfa ? { newPassword, code } : { currentPassword, newPassword },
        ),
    { onError: showOnField },
  );
  const busy = verify.busy || change.busy;
  const loading = verify.loading || change.loading;
  const error = step === "current" ? verify.error : change.error;
  const first = step === "current" || (mfa && step === "new");
  function clearSecrets() {
    setCurrentPassword("");
    setNewPassword("");
    setConfirmation("");
    setCode("");
  }
  function close() {
    if (busy) return;
    clearSecrets();
    onClose();
  }
  function goTo(next: typeof step) {
    verify.error.clear();
    change.error.clear();
    setFieldErrors({});
    setStep(next);
  }
  async function submitCurrent() {
    if ((await verify.run()).ok) goTo("new");
  }
  async function submitNew() {
    const problems = newPasswordProblems(newPassword, confirmation);
    setFieldErrors(problems);
    if (Object.keys(problems).length > 0) return;
    if (mfa) goTo("code");
    else await save();
  }
  async function save() {
    const result = await change.run();
    if (!result.ok) return;
    renew(result.value);
    toast.success("Password updated. Other sessions were signed out.");
    clearSecrets();
    onClose();
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    if (step === "current") return submitCurrent();
    if (step === "new") return submitNew();
    return save();
  }
  function back() {
    if (busy) return;
    goTo(step === "code" ? "new" : "current");
  }
  return (
    <Dialog open={open} onClose={close}>
      <form onSubmit={submit}>
        <DialogTitle>Change password</DialogTitle>
        <DialogDescription>
          {step === "current" && "Enter your current password to continue."}
          {step === "new" &&
            "Choose a new password. Other sessions will be signed out."}
          {step === "code" &&
            "Enter the code from your authenticator app to confirm."}
        </DialogDescription>
        <DialogBody>
          <ErrorNotice error={error} className="mb-6" />
          <UsernameHint />
          {step === "current" && (
            <PasswordField
              label="Current password"
              autoComplete="current-password"
              value={currentPassword}
              onChange={setCurrentPassword}
              error={fieldErrors.currentPassword}
              disabled={loading}
            />
          )}
          {step === "new" && (
            <NewPasswordFields
              password={newPassword}
              confirmation={confirmation}
              onPasswordChange={setNewPassword}
              onConfirmationChange={setConfirmation}
              errors={fieldErrors}
              disabled={loading}
            />
          )}
          {step === "code" && (
            <AppOrRecoveryCode
              value={code}
              onChange={setCode}
              error={fieldErrors.code}
              disabled={loading}
            />
          )}
        </DialogBody>
        <DialogActions>
          {first ? (
            <Button plain onClick={close} disabled={loading}>
              Cancel
            </Button>
          ) : (
            <Button plain disabled={loading} onClick={back}>
              Back
            </Button>
          )}
          <Button
            type="submit"
            loading={loading}
            disabled={step === "code" && code.trim() === ""}
            disabledReason="Fill in required fields"
          >
            {step === "code" || (step === "new" && !mfa) ? "Confirm" : "Next"}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
// Whether two-factor authentication is on, and turning it on or off or
// replacing the recovery codes. The dialogs stay mounted while the session
// switches between off and on underneath them.
function MfaControls() {
  const session = useSession();
  const enabledAt = session.user.mfaEnabledAt;
  const [dialog, setDialog] = useState<"enable" | "disable" | "codes" | null>(
    null,
  );
  const close = () => setDialog(null);
  // Each opening starts a fresh dialog at its first step.
  const enabling = useOpenCount(dialog === "enable");
  const disabling = useOpenCount(dialog === "disable");
  const renewing = useOpenCount(dialog === "codes");
  return (
    <div className="space-y-4">
      {enabledAt && (
        <div className="flex items-center justify-between gap-4">
          <div>
            <div className="text-base/6 font-medium text-zinc-950 sm:text-sm/6 dark:text-white">
              Authenticator app
            </div>
            <Text>On since {formatDate(enabledAt)}</Text>
          </div>
          <Badge color="lime">On</Badge>
        </div>
      )}
      <div className="flex flex-wrap justify-end gap-3">
        {enabledAt ? (
          <>
            <Button outline onClick={() => setDialog("codes")}>
              New recovery codes
            </Button>
            <Button outline onClick={() => setDialog("disable")}>
              Disable
            </Button>
          </>
        ) : (
          <Button outline onClick={() => setDialog("enable")}>
            Enable
          </Button>
        )}
      </div>
      <EnableMfaDialog
        key={`enable-${enabling}`}
        open={dialog === "enable"}
        onClose={close}
      />
      <DisableMfaDialog
        key={`disable-${disabling}`}
        open={dialog === "disable"}
        onClose={close}
      />
      <RecoveryCodesDialog
        key={`codes-${renewing}`}
        open={dialog === "codes"}
        onClose={close}
      />
    </div>
  );
}
// An ApiError about the password or the code, kept for its field.
function mfaFieldErrors(e: unknown): Record<string, string> | null {
  const field = e instanceof ApiError ? e.field : undefined;
  if (field === "password" || field === "code")
    return { [field]: errorMessage(e) };
  return null;
}
// Hidden, so password managers know which account the password belongs to.
function UsernameHint() {
  const session = useSession();
  return (
    <input
      type="email"
      name="username"
      autoComplete="username"
      value={session.user.email}
      readOnly
      hidden
    />
  );
}
// Three steps: the password (which starts the setup), the QR code and the
// first code from the app (which turns MFA on and signs out other sessions),
// then the recovery codes. That last step can't be closed until they've been
// revealed, copied or downloaded: they're never shown again.
function EnableMfaDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const api = useApi();
  const session = useSession();
  const renew = useRenewSession();
  const [step, setStep] = useState<"password" | "scan" | "codes">("password");
  const [password, setPassword] = useState("");
  const [setup, setSetup] = useState<MfaSetup | null>(null);
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[]>([]);
  const [saved, setSaved] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  function showOnField(e: unknown): boolean {
    const errors = mfaFieldErrors(e);
    if (errors) setFieldErrors(errors);
    return errors !== null;
  }
  const start = useAction(() => api.auth().mfa().setup(password), {
    onError: showOnField,
  });
  const enable = useAction(() => api.auth().mfa().store(code), {
    onError: showOnField,
  });
  const busy = start.busy || enable.busy;
  const loading = start.loading || enable.loading;
  function close() {
    if (busy || step === "codes") return;
    onClose();
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    setFieldErrors({});
    if (step === "password") {
      const result = await start.run();
      if (!result.ok) return;
      setPassword("");
      setSetup(result.value);
      setStep("scan");
    } else if (step === "scan") {
      const result = await enable.run();
      if (!result.ok) return;
      setSetup(null);
      setCodes(result.value.recoveryCodes);
      setStep("codes");
      renew({
        ...session,
        user: { ...session.user, mfaEnabledAt: new Date().toISOString() },
      });
    } else {
      toast.success(
        "Two-factor authentication is on. Other sessions were signed out.",
      );
      onClose();
    }
  }
  return (
    <Dialog open={open} onClose={close}>
      <form onSubmit={submit}>
        <DialogTitle>Enable two-factor authentication</DialogTitle>
        <DialogDescription>
          {step === "password" && "Enter your password to continue."}
          {step === "scan" &&
            "Add your account to an authenticator app, then enter the code it shows."}
          {step === "codes" &&
            "Two-factor authentication is on. Save your recovery codes before you go on."}
        </DialogDescription>
        <DialogBody>
          <ErrorNotice
            error={step === "password" ? start.error : enable.error}
            className="mb-6"
          />
          <UsernameHint />
          {step === "password" && (
            <PasswordField
              label="Your password"
              autoComplete="current-password"
              value={password}
              onChange={setPassword}
              error={fieldErrors.password}
              disabled={loading}
            />
          )}
          {step === "scan" && setup && (
            <MfaScanStep
              setup={setup}
              code={code}
              onCodeChange={setCode}
              error={fieldErrors.code}
              disabled={loading}
            />
          )}
          {step === "codes" && (
            <RecoveryCodeList
              codes={codes}
              account={session.user.email}
              onSaved={() => setSaved(true)}
            />
          )}
        </DialogBody>
        <DialogActions>
          {step !== "codes" && (
            <Button plain onClick={close} disabled={loading}>
              Cancel
            </Button>
          )}
          <Button
            type="submit"
            loading={loading}
            disabled={step === "codes" && !saved}
          >
            {step === "password" && "Next"}
            {step === "scan" && "Verify"}
            {step === "codes" && "I’ve saved these codes"}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
// Turning MFA off takes the password and a code (an authenticator or a
// recovery code), so a stolen session can't do it.
function DisableMfaDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const api = useApi();
  const session = useSession();
  const renew = useRenewSession();
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  async function disable() {
    setFieldErrors({});
    await api.auth().mfa().destroy({ password, code });
    renew({ ...session, user: { ...session.user, mfaEnabledAt: null } });
  }
  return (
    <ConfirmationDialog
      open={open}
      onClose={onClose}
      title="Disable two-factor authentication?"
      description="Signing in will only need your password."
      confirm="Disable two-factor authentication"
      disabled={password === "" || code.trim() === ""}
      onConfirm={disable}
      onConfirmed={() => toast.success("Two-factor authentication is off.")}
      onError={(e) => {
        const errors = mfaFieldErrors(e);
        if (errors) setFieldErrors(errors);
        return errors !== null;
      }}
    >
      <UsernameHint />
      <FieldGroup>
        <PasswordField
          label="Your password"
          autoComplete="current-password"
          value={password}
          onChange={setPassword}
          error={fieldErrors.password}
          disabled={false}
        />
        <AppOrRecoveryCode
          value={code}
          onChange={setCode}
          error={fieldErrors.code}
          disabled={false}
        />
      </FieldGroup>
    </ConfirmationDialog>
  );
}
// New recovery codes replace every old one: the password and a code first,
// then the new codes, shown once.
function RecoveryCodesDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const api = useApi();
  const session = useSession();
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const [saved, setSaved] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const create = useAction(
    () => api.auth().mfa().recoveryCodes().store({ password, code }),
    {
      onError: (e) => {
        const errors = mfaFieldErrors(e);
        if (errors) setFieldErrors(errors);
        return errors !== null;
      },
    },
  );
  function close() {
    if (create.busy || codes) return;
    onClose();
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (codes) {
      toast.success("New recovery codes saved. The old ones no longer work.");
      onClose();
      return;
    }
    setFieldErrors({});
    const result = await create.run();
    if (!result.ok) return;
    setPassword("");
    setCode("");
    setCodes(result.value.recoveryCodes);
  }
  return (
    <Dialog open={open} onClose={close}>
      <form onSubmit={submit}>
        <DialogTitle>New recovery codes</DialogTitle>
        <DialogDescription>
          {codes
            ? "Your old recovery codes no longer work. Save these before you go on."
            : "Your current recovery codes will stop working. Enter your password and a code to continue."}
        </DialogDescription>
        <DialogBody>
          <ErrorNotice error={create.error} className="mb-6" />
          <UsernameHint />
          {codes ? (
            <RecoveryCodeList
              codes={codes}
              account={session.user.email}
              onSaved={() => setSaved(true)}
            />
          ) : (
            <FieldGroup>
              <PasswordField
                label="Your password"
                autoComplete="current-password"
                value={password}
                onChange={setPassword}
                error={fieldErrors.password}
                disabled={create.loading}
              />
              <AppOrRecoveryCode
                value={code}
                onChange={setCode}
                error={fieldErrors.code}
                disabled={create.loading}
              />
            </FieldGroup>
          )}
        </DialogBody>
        <DialogActions>
          {!codes && (
            <Button plain onClick={close} disabled={create.loading}>
              Cancel
            </Button>
          )}
          <Button
            type="submit"
            loading={create.loading}
            disabled={codes !== null && !saved}
          >
            {codes ? "I’ve saved these codes" : "Create new codes"}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
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
      <div className="flex justify-end">
        <Button outline loading={revoke.loading} onClick={revokeOthers}>
          Sign out other sessions
        </Button>
      </div>
    </div>
  );
}
// "Download my data": the password in a dialog, then the API's answer saved
// as a file. Nothing of it stays in the page's state once saved.
function DataExportControls() {
  const [open, setOpen] = useState(false);
  // Each opening starts with an empty password.
  const [opened, setOpened] = useState(0);
  return (
    <div className="flex items-start justify-end">
      <Button
        outline
        onClick={() => {
          setOpened(opened + 1);
          setOpen(true);
        }}
      >
        Download your data
      </Button>
      <DataExportDialog
        key={opened}
        open={open}
        onClose={() => setOpen(false)}
      />
    </div>
  );
}
function DataExportDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const api = useApi();
  const session = useSession();
  const [password, setPassword] = useState("");
  const [passwordError, setPasswordError] = useState<string>();
  function showOnField(e: unknown): boolean {
    if (e instanceof ApiError && e.field === "password") {
      setPasswordError(errorMessage(e));
      return true;
    }
    return false;
  }
  async function download() {
    setPasswordError(undefined);
    const data = await api.auth().exportData({ password });
    saveJson(
      data,
      `lewcontext-export-${new Date().toISOString().slice(0, 10)}.json`,
    );
  }
  return (
    <ConfirmationDialog
      open={open}
      onClose={onClose}
      title="Download your data"
      description="Your profile, sign-in sessions, memberships, the workspaces you own with their projects and the invitations you sent, and your account’s activity. Enter your password to download it; you can do this five times a day."
      confirm="Download"
      disabled={password === ""}
      onConfirm={download}
      onConfirmed={() => toast.success("Your data has been downloaded.")}
      onError={showOnField}
    >
      {/* Tells password managers which account the password belongs to. */}
      <input
        type="email"
        name="username"
        autoComplete="username"
        value={session.user.email}
        readOnly
        hidden
      />
      <PasswordField
        label="Your password"
        autoComplete="current-password"
        value={password}
        onChange={setPassword}
        error={passwordError}
        disabled={false}
      />
    </ConfirmationDialog>
  );
}
// saveJson hands data to the browser as a file to save.
function saveJson(data: unknown, name: string) {
  const blob = new Blob([JSON.stringify(data, null, 2) + "\n"], {
    type: "application/json",
  });
  const href = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = href;
  link.download = name;
  link.click();
  URL.revokeObjectURL(href);
}
// The workspaces you're the only owner of block deleting your account, so the
// page loads them first and opens a warning instead of the confirmation. The
// API refuses the same way (409) if they changed since, and the warning then
// takes over from the confirmation.
function DeleteAccountControls() {
  const stuck = useResource(
    paths.workspaces.index({ canLeave: false, perPage: 100 }),
  );
  // Every workspace you're in, for the confirmation's count.
  const all = useResource(paths.workspaces.index());
  const [dialog, setDialog] = useState<"confirm" | "last-owner" | null>(null);
  // Each opening starts with an empty password.
  const [opened, setOpened] = useState(0);
  // From the API's 409, when it knows better than the loaded list.
  const [refused, setRefused] = useState<WorkspaceSummary[] | null>(null);
  const blocking = refused ?? stuck.data?.data ?? [];
  function open() {
    setOpened(opened + 1);
    setDialog(blocking.length > 0 ? "last-owner" : "confirm");
  }
  return (
    <>
      <DangerAction
        title="Delete account"
        description={
          blocking.length > 0
            ? `You’re the only owner of ${blocking.length === 1 ? "a workspace" : `${blocking.length} workspaces`}. Delete ${blocking.length === 1 ? "it" : "them"}, or make someone else an owner, first.`
            : "Deletes your account and removes you from every workspace you’re in."
        }
        action={
          <Button color="red" loading={stuck.loading} onClick={open}>
            Delete account
          </Button>
        }
      />
      <DeleteAccountDialog
        key={opened}
        open={dialog === "confirm"}
        workspaces={all.data?.pages.items}
        onClose={() => setDialog(null)}
        onLastOwner={(workspaces) => {
          setRefused(workspaces);
          setDialog("last-owner");
        }}
      />
      <LastOwnerDialog
        open={dialog === "last-owner"}
        workspaces={blocking}
        onClose={() => setDialog(null)}
      />
    </>
  );
}
// Shown instead of the confirmation while you're the only owner of a
// workspace: each links to its settings (Danger zone, Members), and there's
// no way to delete from here.
function LastOwnerDialog({
  open,
  workspaces,
  onClose,
}: {
  open: boolean;
  workspaces: WorkspaceSummary[];
  onClose: () => void;
}) {
  return (
    <Dialog open={open} onClose={onClose} size="md">
      <DialogTitle>Delete your workspaces first</DialogTitle>
      <DialogDescription>
        You’re the only owner of these workspaces. Delete them, or make someone
        else an owner, before deleting your account.
      </DialogDescription>
      <DialogBody>
        <ul className="divide-y divide-zinc-950/5 dark:divide-white/5">
          {workspaces.map((workspace) => (
            <li key={workspace.id}>
              <Link
                href={routes.workspaces.edit(workspace.id)}
                className="flex items-center justify-between gap-4 py-3 text-base/6 font-medium text-zinc-950 data-hover:text-zinc-600 sm:text-sm/6 dark:text-white dark:data-hover:text-zinc-300"
              >
                <span className="flex min-w-0 items-center gap-3">
                  <WorkspaceAvatar workspace={workspace} className="size-6" />
                  <span className="truncate">{workspace.name}</span>
                </span>
                <ChevronRightIcon className="size-4 shrink-0 fill-zinc-400" />
              </Link>
            </li>
          ))}
        </ul>
      </DialogBody>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}
// "Terms and privacy", only while the policies are published: what the
// account agreed to at sign-up, a document updated since, or, without a
// record (made before they were published), links to read them. Read-only.
function PoliciesSection() {
  const status = useResource(paths.policies.show());
  const agreed = useSession().user.policies;
  if (!status.data?.enabled) return null;
  const current = status.data;
  const terms = (
    <TextLink href={routes.termsOfService}>Terms of Service</TextLink>
  );
  const privacy = (
    <TextLink href={routes.privacyPolicy}>Privacy Policy</TextLink>
  );
  const updated = [
    agreed && agreed.termsOfService !== current.termsOfService
      ? `The Terms of Service were updated on ${formatDate(current.termsOfService, "long")}.`
      : null,
    agreed && agreed.privacyPolicy !== current.privacyPolicy
      ? `The Privacy Policy was updated on ${formatDate(current.privacyPolicy, "long")}.`
      : null,
  ].filter(Boolean);
  return (
    <>
      <Divider className="my-10" soft />
      <SettingsSection title="Terms and privacy">
        <div className="space-y-2">
          {agreed ? (
            <Text>
              You agreed to the {terms} (
              {formatDate(agreed.termsOfService, "long")}) and read the{" "}
              {privacy} ({formatDate(agreed.privacyPolicy, "long")}) on{" "}
              {formatDate(agreed.acceptedAt, "long")}.
            </Text>
          ) : (
            <Text>
              Read our {terms} and {privacy}.
            </Text>
          )}
          {updated.map((line) => (
            <Text key={line}>{line}</Text>
          ))}
        </div>
      </SettingsSection>
    </>
  );
}
