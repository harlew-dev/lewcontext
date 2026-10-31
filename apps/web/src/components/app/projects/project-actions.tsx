import { useState } from "react";
import { useSWRConfig } from "swr";
import {
  EllipsisHorizontalIcon,
  PencilSquareIcon,
  TrashIcon,
} from "@heroicons/react/16/solid";
import { ConfirmationDialog } from "@/components/ui/confirmation-dialog";
import {
  Dropdown,
  DropdownButton,
  DropdownDivider,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { toast } from "@/components/ui/toast";
import { paths, useApi } from "@/lib/api";
import { useOpenCount } from "@/lib/use-open-count";
import { useWorkspace } from "@/lib/workspace";
import type { Project } from "@/types/projects";
import { can, type Workspace } from "@/types/workspaces";
// The ⋯ menu for one project, in each project table row and on the project's
// page: Edit project (with `onEdit` and project.update, which opens the
// page's ProjectDialog) and Delete project (with project.destroy, after a
// confirmation). Nothing renders when neither is allowed. After deleting,
// every list and count of the workspace's projects refreshes; `onDeleted`
// runs first (the project's own page leaves it).
// Whether a member may do anything in the ⋯ menu: tables give it a column.
export function hasProjectActions(workspace: Workspace): boolean {
  return can(workspace, "project.update") || can(workspace, "project.destroy");
}
export function ProjectActions({
  project,
  onEdit,
  onDeleted,
}: {
  project: Project;
  onEdit?: () => void;
  onDeleted?: () => unknown;
}) {
  const api = useApi();
  const workspace = useWorkspace();
  const { mutate } = useSWRConfig();
  const [open, setOpen] = useState(false);
  // Each opening starts without the last one's error.
  const opened = useOpenCount(open);
  const canEdit = onEdit !== undefined && can(workspace, "project.update");
  const canDelete = can(workspace, "project.destroy");
  if (!canEdit && !canDelete) return null;
  const projects = paths.projects(workspace.id);
  // SWR keys are [path, userId].
  const keyPath = (key: unknown) =>
    Array.isArray(key) && typeof key[0] === "string" ? key[0] : null;
  async function deleted() {
    toast.success("Project deleted.");
    // Its own page must show "not found" from now on, not cached data.
    const show = projects.show(project.id);
    await mutate((key) => keyPath(key) === show, undefined, {
      revalidate: false,
    });
    await onDeleted?.();
    void mutate((key) => {
      const path = keyPath(key);
      return path !== null && projects.matches(path);
    });
  }
  return (
    <>
      <Dropdown>
        <DropdownButton plain aria-label={`Actions for ${project.name}`}>
          <EllipsisHorizontalIcon />
        </DropdownButton>
        <DropdownMenu anchor="bottom end">
          {canEdit && (
            <DropdownItem onClick={onEdit}>
              <PencilSquareIcon />
              <DropdownLabel>Edit project</DropdownLabel>
            </DropdownItem>
          )}
          {canEdit && canDelete && <DropdownDivider />}
          {canDelete && (
            <DropdownItem variant="danger" onClick={() => setOpen(true)}>
              <TrashIcon />
              <DropdownLabel>Delete project</DropdownLabel>
            </DropdownItem>
          )}
        </DropdownMenu>
      </Dropdown>
      <ConfirmationDialog
        key={opened}
        open={open}
        onClose={() => setOpen(false)}
        title={`Delete ${project.name}?`}
        description="This permanently deletes the project. It can’t be undone."
        confirm="Delete project"
        variant="danger"
        onConfirm={() => api.projects(workspace.id).destroy(project.id)}
        onConfirmed={deleted}
      />
    </>
  );
}
