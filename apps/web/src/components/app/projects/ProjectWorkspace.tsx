import type { ReactNode } from "react";
import { Plus } from "lucide-react";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/Button";
import { PageHeading } from "@/components/ui/Typography";
import { Notice } from "@/components/ui/Feedback";
import type { WorkspaceProps } from "@/lib/types";
import { ProjectDialog } from "./ProjectDialog";
import type { ProjectsState } from "./useProjects";
// Page frame shared by every project-backed page: heading with "New project",
// mutation notices, load errors with retry, and the create/edit dialog.
export function ProjectWorkspace({
  session,
  state,
  title,
  eyebrow,
  heading,
  description,
  intro,
  children,
}: Pick<WorkspaceProps, "session"> & {
  state: ProjectsState;
  title: string;
  eyebrow: string;
  heading: string;
  description: string;
  intro?: ReactNode;
  children: ReactNode;
}) {
  return (
    <AppShell session={session} title={title}>
      <PageHeading
        eyebrow={eyebrow}
        title={heading}
        description={description}
        action={
          <Button
            onClick={() => state.setDialog("new")}
            startEnhancer={() => <Plus size={16} />}
          >
            New project
          </Button>
        }
      />
      {state.notice && (
        <Notice variant="success" onDismiss={() => state.setNotice("")}>
          {state.notice}
        </Notice>
      )}
      {intro}
      {state.error && (
        <Notice variant="error" onRetry={state.refresh}>
          {state.error}
        </Notice>
      )}
      {children}
      {state.dialog && (
        <ProjectDialog
          project={state.dialog === "new" ? undefined : state.dialog}
          csrfToken={session.csrfToken}
          onClose={() => state.setDialog(null)}
          onSaved={state.saved}
        />
      )}
    </AppShell>
  );
}
