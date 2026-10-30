import { useState, type FormEvent } from "react";
import { useRouter } from "next/router";
import { CheckIcon, DocumentDuplicateIcon } from "@heroicons/react/16/solid";
import { appLayout } from "@/components/layout/application-layout";
import { ServiceUnavailable } from "@/components/layout/status-page";
import { WorkspaceNameField } from "@/components/app/workspaces/workspace-form";
import { Badge, type BadgeColor } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Divider } from "@/components/ui/divider";
import { Notice } from "@/components/ui/feedback";
import { Field, Label } from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { SettingsSection } from "@/components/ui/settings-section";
import { Strong, Text } from "@/components/ui/text";
import { useApi } from "@/lib/api";
import type { AppPageProps } from "@/lib/types";
import { useActionError } from "@/lib/use-action-error";
import { useRefreshWorkspaces, useWorkspace } from "@/lib/workspace";
import { canManage, workspaceName, type Role } from "@/types/workspaces";
export { sessionProps as getServerSideProps } from "@/lib/server";
export default function WorkspaceSettings(props: AppPageProps) {
  if ("unavailable" in props) return <ServiceUnavailable />;
  return <WorkspaceSettingsContent />;
}
WorkspaceSettings.getLayout = appLayout;
const roleBadges: Record<Role, { label: string; color: BadgeColor }> = {
  owner: { label: "Owner", color: "purple" },
  admin: { label: "Admin", color: "pink" },
  member: { label: "Member", color: "zinc" },
};
function WorkspaceSettingsContent() {
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
          <Text>
            <Strong>{workspace.name}</Strong>
          </Text>
        )}
      </SettingsSection>
      <Divider className="my-10" soft />
      <SettingsSection
        title="Workspace ID"
        description="Use it to refer to this workspace, for example when asking for help."
      >
        <CopyField label="Workspace ID" value={workspace.id} />
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
// Renames the workspace, then reloads the page's props and the switcher so
// the new name shows everywhere.
function RenameForm() {
  const api = useApi();
  const router = useRouter();
  const workspace = useWorkspace();
  const refresh = useRefreshWorkspaces();
  const [name, setName] = useState(workspace.name);
  const [saving, setSaving] = useState(false);
  const [done, setDone] = useState(false);
  const error = useActionError();
  const clean = workspaceName(name);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (clean === null || saving) return;
    setSaving(true);
    setDone(false);
    try {
      await api.workspaces().update(workspace.id, { name: clean });
      error.clear();
      void refresh();
      await router.replace(router.asPath, undefined, { scroll: false });
      setName(clean);
      setDone(true);
    } catch (e) {
      error.fail(e);
    } finally {
      setSaving(false);
    }
  }
  return (
    <form onSubmit={submit} className="space-y-4">
      <WorkspaceNameField
        label="Name"
        value={name}
        onChange={(value) => {
          setName(value);
          setDone(false);
        }}
        disabled={saving}
      />
      <Button
        type="submit"
        loading={saving}
        disabled={clean === null || clean === workspace.name}
      >
        Save
      </Button>
      {error.message && (
        <Notice key={error.key} variant="error">
          {error.message}
        </Notice>
      )}
      {done && <Notice variant="success">Workspace renamed.</Notice>}
    </form>
  );
}
// A read-only value with a Copy button. Where the clipboard isn't available
// the field stays selectable, so the value can still be copied by hand.
function CopyField({ label, value }: { label: string; value: string }) {
  const [status, setStatus] = useState("");
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
      <div className="flex items-end gap-2">
        <Field className="flex-1">
          <Label className="sr-only">{label}</Label>
          <Input
            readOnly
            value={value}
            className="font-mono"
            onFocus={(e) => e.currentTarget.select()}
          />
        </Field>
        <Button outline onClick={copy} aria-label={`Copy ${label}`}>
          {status === "Copied" ? <CheckIcon /> : <DocumentDuplicateIcon />}
          Copy
        </Button>
      </div>
      <p role="status" className="text-sm/6 text-zinc-500 dark:text-zinc-400">
        {status}
      </p>
    </div>
  );
}
