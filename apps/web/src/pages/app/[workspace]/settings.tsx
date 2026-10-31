import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { CheckIcon, DocumentDuplicateIcon } from "@heroicons/react/16/solid";
import { SettingsTabs } from "@/components/app/workspaces/settings-tabs";
import { WorkspaceNameField } from "@/components/app/workspaces/workspace-form";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogActions,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Divider } from "@/components/ui/divider";
import { ErrorNotice } from "@/components/ui/feedback";
import { Field, Label } from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import {
  Input,
  InputActions,
  InputButton,
  InputGroup,
  InputLock,
} from "@/components/ui/input";
import { SettingsSection } from "@/components/ui/settings-section";
import { toast } from "@/components/ui/toast";
import { Tooltip } from "@/components/ui/tooltip";
import { useApi } from "@/lib/api";
import { cx } from "@/lib/cx";
import { routes } from "@/lib/routes";
import { useAction } from "@/lib/use-action";
import { useRefreshWorkspaces, useWorkspace } from "@/lib/workspace";
import { can, workspaceName } from "@/types/workspaces";
export default function WorkspaceSettings() {
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
      <LeaveSection key={workspace.id} />
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
      <Button
        type="submit"
        loading={rename.loading}
        disabled={clean === null || clean === workspace.name}
      >
        Save
      </Button>
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
// Leaving takes you out of the workspace at once; /app then opens your next
// workspace, or onboarding when there is none. That page is the confirmation:
// onboarding has no toaster.
function LeaveSection() {
  const api = useApi();
  const workspace = useWorkspace();
  const refresh = useRefreshWorkspaces();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const leave = useAction(() => api.workspaces().leave(workspace.id));
  async function confirm() {
    if (!(await leave.run()).ok) return;
    refresh();
    await navigate(routes.app);
  }
  const close = () => !leave.busy && setOpen(false);
  return (
    <SettingsSection
      title="Leave workspace"
      description={
        !workspace.canLeave
          ? "You’re its only owner. Make someone else an owner before you leave."
          : "You lose access to this workspace and its projects."
      }
    >
      <div>
        <Button
          outline
          disabled={!workspace.canLeave}
          onClick={() => setOpen(true)}
        >
          Leave workspace
        </Button>
      </div>
      <Dialog open={open} onClose={close} size="md">
        <DialogTitle>Leave {workspace.name}?</DialogTitle>
        <DialogDescription>
          You lose access at once. To come back, someone will need to add you
          again.
        </DialogDescription>
        <ErrorNotice error={leave.error} className="mt-6" />
        <DialogActions>
          <Button plain onClick={close} disabled={leave.loading}>
            Cancel
          </Button>
          <Button color="red" loading={leave.loading} onClick={confirm}>
            Leave workspace
          </Button>
        </DialogActions>
      </Dialog>
    </SettingsSection>
  );
}
