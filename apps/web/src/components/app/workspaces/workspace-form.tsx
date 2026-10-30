import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { ErrorNotice } from "@/components/ui/feedback";
import { Field, Label } from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";
import { useApi } from "@/lib/api";
import { routes } from "@/lib/routes";
import { useAction } from "@/lib/use-action";
import { useRefreshWorkspaces } from "@/lib/workspace";
import { maxWorkspaceName, workspaceName } from "@/types/workspaces";
// Creating a workspace, from onboarding or the switcher's dialog: sends the
// name, refreshes the switcher's list and opens the new workspace. The form
// stays loading until the new workspace's page has loaded (the action awaits
// the navigation); then `onOpened` runs (the dialog closes).
export function useCreateWorkspace(onOpened?: () => void) {
  const api = useApi();
  const navigate = useNavigate();
  const refresh = useRefreshWorkspaces();
  const [name, setName] = useState("");
  const create = useAction(async (clean: string) => {
    const workspace = await api.workspaces().create({ name: clean });
    refresh();
    await navigate(routes.dashboard(workspace.id));
    onOpened?.();
  });
  const valid = workspaceName(name) !== null;
  async function submit(event: FormEvent) {
    event.preventDefault();
    const clean = workspaceName(name);
    if (clean !== null) await create.run(clean);
  }
  return {
    name,
    setName,
    busy: create.busy,
    loading: create.loading,
    valid,
    error: create.error,
    submit,
  };
}
export type WorkspaceCreator = ReturnType<typeof useCreateWorkspace>;
// The workspace name input, shared by creating and renaming.
export function WorkspaceNameField({
  value,
  onChange,
  disabled,
  autoFocus,
  label = "Workspace name",
}: {
  value: string;
  onChange: (name: string) => void;
  disabled?: boolean;
  autoFocus?: boolean;
  label?: string;
}) {
  return (
    <Field disabled={disabled}>
      <Label>{label}</Label>
      <Input
        name="workspace"
        autoFocus={autoFocus}
        required
        maxLength={maxWorkspaceName}
        autoComplete="off"
        placeholder="e.g. Acme"
        value={value}
        onChange={(e) => onChange(e.currentTarget.value)}
      />
    </Field>
  );
}
// The switcher's "New workspace…" dialog. Its form state starts fresh each
// time it opens.
export function CreateWorkspaceDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [opened, setOpened] = useState(0);
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setOpened(opened + 1);
  }
  return <CreateWorkspaceForm key={opened} open={open} onClose={onClose} />;
}
function CreateWorkspaceForm({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const create = useCreateWorkspace(onClose);
  // Not while the workspace is being created, even before it shows loading.
  const close = () => !create.busy && onClose();
  return (
    <Dialog open={open} onClose={close}>
      <form onSubmit={create.submit}>
        <DialogTitle>New workspace</DialogTitle>
        <DialogDescription>
          A fresh space for another team or another big idea.
        </DialogDescription>
        <DialogBody>
          <ErrorNotice error={create.error} className="mb-6" />
          <WorkspaceNameField
            autoFocus
            value={create.name}
            onChange={create.setName}
            disabled={create.loading}
          />
        </DialogBody>
        <DialogActions>
          <Button plain onClick={close} disabled={create.loading}>
            Cancel
          </Button>
          <Button
            type="submit"
            loading={create.loading}
            disabled={!create.valid}
          >
            Create workspace
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}
