import { useState, type FormEvent } from "react";
import { CheckIcon, DocumentDuplicateIcon } from "@heroicons/react/16/solid";
import { WorkspaceNameField } from "@/components/app/workspaces/workspace-form";
import { Badge, type BadgeColor } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { useAction } from "@/lib/use-action";
import { useRefreshWorkspaces, useWorkspace } from "@/lib/workspace";
import { canManage, workspaceName, type Role } from "@/types/workspaces";
const roleBadges: Record<Role, { label: string; color: BadgeColor }> = {
  owner: { label: "Owner", color: "purple" },
  admin: { label: "Admin", color: "pink" },
  member: { label: "Member", color: "zinc" },
};
export default function WorkspaceSettings() {
  const workspace = useWorkspace();
  const role = roleBadges[workspace.role];
  return (
    <div className="mx-auto max-w-4xl">
      <Heading>Workspace settings</Heading>
      <Divider className="my-10 mt-6" />
      <SettingsSection
        title="General"
        description={
          canManage(workspace.role)
            ? "The name everyone in this workspace sees."
            : "Only owners and admins can rename this workspace."
        }
      >
        {canManage(workspace.role) ? (
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
      <SettingsSection
        title="Your role"
        description="What you can do in this workspace."
      >
        <div>
          <Badge color={role.color}>{role.label}</Badge>
        </div>
      </SettingsSection>
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
