import { useState, type FormEvent } from "react";
import {
  ChevronRightIcon,
  EyeIcon,
  EyeSlashIcon,
} from "@heroicons/react/16/solid";
import { DeleteAccountDialog } from "@/components/app/account/delete-account-dialog";
import {
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
import { ErrorNotice } from "@/components/ui/feedback";
import { Field, FieldGroup, Label } from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import {
  Input,
  InputActions,
  InputButton,
  InputGroup,
  InputLock,
} from "@/components/ui/input";
import { Link } from "@/components/ui/link";
import {
  DangerAction,
  SettingsSection,
} from "@/components/ui/settings-section";
import { Text } from "@/components/ui/text";
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
// Saves the name, then renews the session with the updated user so the new
// name shows everywhere. The email address is how the user signs in, so it
// can't be changed here; it starts blurred, in case the screen is shared, and
// the eye shows it.
function ProfileForm() {
  const api = useApi();
  const session = useSession();
  const renew = useRenewSession();
  const [name, setName] = useState(session.user.name);
  const [emailVisible, setEmailVisible] = useState(false);
  const save = useAction((clean: string) =>
    api.auth().updateProfile({ name: clean }),
  );
  const clean = cleanName(name);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (clean === null) return;
    const result = await save.run(clean);
    if (!result.ok) return;
    renew({ ...session, user: result.value });
    setName(result.value.name);
    toast.success("Profile updated.");
  }
  return (
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
        <Field>
          <Label>Email address</Label>
          <InputGroup>
            <Input
              type="email"
              value={session.user.email}
              readOnly
              blurred={!emailVisible}
              onReveal={() => setEmailVisible(true)}
            />
            <InputActions>
              <Tooltip content={emailVisible ? "Hide" : "Reveal"}>
                <InputButton
                  aria-label={
                    emailVisible ? "Hide email address" : "Reveal email address"
                  }
                  aria-pressed={emailVisible}
                  onClick={() => setEmailVisible(!emailVisible)}
                >
                  {emailVisible ? <EyeSlashIcon /> : <EyeIcon />}
                </InputButton>
              </Tooltip>
              <InputLock label="Your sign-in email can’t be changed" />
            </InputActions>
          </InputGroup>
        </Field>
      </FieldGroup>
      <div className="flex justify-end">
        <Button
          type="submit"
          loading={save.loading}
          disabled={clean === null || clean === session.user.name}
        >
          Save
        </Button>
      </div>
      <ErrorNotice error={save.error} />
    </form>
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
            <NewPasswordFields
              password={newPassword}
              confirmation={confirmation}
              onPasswordChange={setNewPassword}
              onConfirmationChange={setConfirmation}
              errors={fieldErrors}
              disabled={loading}
            />
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
        <CodeField
          label="Authentication or recovery code"
          value={code}
          onChange={setCode}
          error={fieldErrors.code}
          disabled={false}
          recovery
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
              <CodeField
                label="Authentication or recovery code"
                value={code}
                onChange={setCode}
                error={fieldErrors.code}
                disabled={create.loading}
                recovery
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
