import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { CheckIcon, DocumentDuplicateIcon } from "@heroicons/react/16/solid";
import { PasswordField } from "@/components/app/account/password-fields";
import { SettingsTabs } from "@/components/app/workspaces/settings-tabs";
import { WorkspaceNameField } from "@/components/app/workspaces/workspace-form";
import { Button } from "@/components/ui/button";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
import { Divider } from "@/components/ui/divider";
import { ErrorNotice } from "@/components/ui/feedback";
import {
  ErrorMessage,
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
  InputLock,
} from "@/components/ui/input";
import {
  DangerAction,
  SettingsSection,
} from "@/components/ui/settings-section";
import { Strong } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { Tooltip } from "@/components/ui/tooltip";
import { ApiError, errorMessage, useApi } from "@/lib/api";
import { cx } from "@/lib/cx";
import { routes, type OnboardingState } from "@/lib/routes";
import { useAction } from "@/lib/use-action";
import {
  useRefreshWorkspaces,
  useWorkspace,
  useWorkspaceList,
} from "@/lib/workspace";
import { can, workspaceName } from "@/types/workspaces";
export default function WorkspaceEdit() {
  const workspace = useWorkspace();
  const canRename = can(workspace, "workspace.update");
  return (
    <div className="mx-auto max-w-4xl">
      <Heading>Workspace settings</Heading>
      <SettingsTabs current="general" />
      <Divider className="my-10 mt-6" />
      <SettingsSection
        title="General"
        description={
          canRename
            ? "The name everyone in this workspace sees."
            : "Only owners and admins can rename this workspace."
        }
      >
        {canRename ? (
          <RenameForm key={workspace.id} />
        ) : (
          <Field>
            <Label>Name</Label>
            <InputGroup>
              <Input readOnly value={workspace.name} />
              <InputActions>
                <InputLock label="Only owners and admins can rename this" />
              </InputActions>
            </InputGroup>
          </Field>
        )}
      </SettingsSection>
      <Divider className="my-10" soft />
      <SettingsSection
        title="Workspace ID"
        description="Use it to refer to this workspace, for example when asking for help."
      >
        <CopyField
          label="Workspace ID"
          value={workspace.id}
          lock="Workspace IDs can’t be changed"
        />
      </SettingsSection>
      <Divider className="my-10" soft />
      <DangerZone key={workspace.id} />
    </div>
  );
}
// Renames the workspace, then reloads its record and the switcher so the new
// name shows everywhere.
function RenameForm() {
  const api = useApi();
  const workspace = useWorkspace();
  const refresh = useRefreshWorkspaces();
  const [name, setName] = useState(workspace.name);
  const rename = useAction((clean: string) =>
    api.workspaces().update(workspace.id, { name: clean }),
  );
  const clean = workspaceName(name);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (clean === null) return;
    if (!(await rename.run(clean)).ok) return;
    refresh(workspace.id);
    setName(clean);
    toast.success("Workspace renamed.");
  }
  return (
    <form onSubmit={submit} className="space-y-4">
      <WorkspaceNameField
        label="Name"
        value={name}
        onChange={setName}
        disabled={rename.loading}
      />
      <div className="flex justify-end">
        <Button
          type="submit"
          loading={rename.loading}
          disabled={clean === null || clean === workspace.name}
        >
          Save
        </Button>
      </div>
      <ErrorNotice error={rename.error} />
    </form>
  );
}
// A read-only value with a Copy button inside the input, and a lock saying
// why it can't change (`lock`). Where the clipboard isn't available the field
// stays selectable, so the value can still be copied by hand.
function CopyField({
  label,
  value,
  lock,
}: {
  label: string;
  value: string;
  lock: string;
}) {
  const [status, setStatus] = useState("");
  const copied = status === "Copied";
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setStatus("Copied");
    } catch {
      setStatus("Couldn’t copy. Select the ID and copy it instead.");
    }
  }
  return (
    <div className="space-y-2">
      <Field>
        <Label className="sr-only">{label}</Label>
        <InputGroup>
          <Input
            readOnly
            value={value}
            className="font-mono"
            onFocus={(e) => e.currentTarget.select()}
          />
          <InputActions>
            <Tooltip content={copied ? "Copied" : "Copy"}>
              <InputButton aria-label={`Copy ${label}`} onClick={copy}>
                {copied ? <CheckIcon /> : <DocumentDuplicateIcon />}
              </InputButton>
            </Tooltip>
            <InputLock label={lock} />
          </InputActions>
        </InputGroup>
      </Field>
      {/* "Copied" shows in the tooltip; screen readers hear it from here. */}
      <p
        role="status"
        className={cx(
          "text-sm/6 text-zinc-500 dark:text-zinc-400",
          copied && "sr-only",
        )}
      >
        {status}
      </p>
    </div>
  );
}
// Leaving, for every member but the only owner, and deleting, for owners.
// Neither can be undone.
function DangerZone() {
  const workspace = useWorkspace();
  const canDelete = can(workspace, "workspace.destroy");
  if (!workspace.canLeave && !canDelete) return null;
  return (
    <SettingsSection
      title="Danger zone"
      description="These take effect at once and can’t be undone."
      variant="danger"
    >
      {workspace.canLeave && <LeaveAction />}
      {canDelete && <DeleteWorkspaceAction />}
    </SettingsSection>
  );
}
// Leaving takes you out of the workspace at once; /app then opens your next
// workspace, or onboarding when there is none. The dialog stays busy until
// that page has loaded. The only owner doesn't see it: they can't leave.
function LeaveAction() {
  const api = useApi();
  const workspace = useWorkspace();
  const refresh = useRefreshWorkspaces();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  async function leave() {
    await api.workspaces().leave(workspace.id);
    refresh();
    await navigate(routes.app);
  }
  return (
    <>
      <DangerAction
        title="Leave workspace"
        description="You lose access to this workspace and its projects."
        action={
          <Button color="red" onClick={() => setOpen(true)}>
            Leave workspace
          </Button>
        }
      />
      <ConfirmationDialog
        open={open}
        onClose={() => setOpen(false)}
        title={`Leave ${workspace.name}?`}
        description="You lose access at once. To come back, someone will need to add you again."
        confirm="Leave workspace"
        variant="danger"
        onConfirm={leave}
      />
    </>
  );
}
function DeleteWorkspaceAction() {
  const [open, setOpen] = useState(false);
  // Each opening starts with empty fields.
  const [opened, setOpened] = useState(0);
  return (
    <>
      <DangerAction
        title="Delete workspace"
        description="Deletes it for everyone, with its projects and memberships."
        action={
          <Button
            color="red"
            onClick={() => {
              setOpened(opened + 1);
              setOpen(true);
            }}
          >
            Delete workspace
          </Button>
        }
      />
      <DeleteWorkspaceDialog
        key={opened}
        open={open}
        onClose={() => setOpen(false)}
      />
    </>
  );
}
// Type the workspace's name, plus your password. The API checks both again:
// the name against the current one, so a stale tab can't delete it under an
// old name. Afterwards your next workspace's dashboard opens, with a toast;
// without one, onboarding opens and says so itself, since the app layout
// (and its toaster) goes away.
function DeleteWorkspaceDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const api = useApi();
  const workspace = useWorkspace();
  const refresh = useRefreshWorkspaces();
  const list = useWorkspaceList();
  const navigate = useNavigate();
  const [typed, setTyped] = useState("");
  const [password, setPassword] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  // A wrong name or password shows under its field.
  function showOnField(e: unknown): boolean {
    const field = e instanceof ApiError ? e.field : undefined;
    if (field !== "name" && field !== "password") return false;
    setFieldErrors({ [field]: errorMessage(e) });
    return true;
  }
  async function destroy() {
    setFieldErrors({});
    await api.workspaces().destroy(workspace.id, { name: typed, password });
    const next = list.items.find((other) => other.id !== workspace.id);
    refresh();
    if (next) {
      await navigate(routes.dashboard(next.id));
      toast.success(`${workspace.name} deleted.`);
      return;
    }
    const state: OnboardingState = { deletedWorkspace: workspace.name };
    await navigate(routes.onboarding, { state });
  }
  return (
    <ConfirmationDialog
      open={open}
      onClose={onClose}
      title={`Delete ${workspace.name}?`}
      description="This permanently deletes the workspace, its projects and every membership, for everyone in it. It can’t be undone."
      confirm="Delete workspace"
      variant="danger"
      disabled={typed.trim() !== workspace.name || password === ""}
      onConfirm={destroy}
      onError={showOnField}
    >
      <FieldGroup>
        <Field>
          <Label>
            Type <Strong>{workspace.name}</Strong> to confirm
          </Label>
          <Input
            autoComplete="off"
            autoFocus
            invalid={Boolean(fieldErrors.name)}
            value={typed}
            onChange={(e) => setTyped(e.currentTarget.value)}
          />
          {fieldErrors.name && (
            <ErrorMessage role="alert">{fieldErrors.name}</ErrorMessage>
          )}
        </Field>
        <PasswordField
          label="Your password"
          autoComplete="current-password"
          autoFocus={false}
          value={password}
          onChange={setPassword}
          error={fieldErrors.password}
          disabled={false}
        />
      </FieldGroup>
    </ConfirmationDialog>
  );
}
