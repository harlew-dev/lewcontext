import { useState } from "react";
import { useSWRConfig } from "swr";
import { apiRoutes } from "@/lib/routes";
import type { Project } from "@/types/projects";
export type ProjectEditor = ReturnType<typeof useProjectEditor>;
// Create/edit dialog state and success notice for project pages. After a save
// every cached project list and project revalidates, since the change can
// move it between pages, filters and counts.
export function useProjectEditor(
  onSaved?: (project: Project, created: boolean) => void,
) {
  const { mutate } = useSWRConfig();
  const [dialog, setDialog] = useState<Project | "new" | null>(null);
  const [notice, setNotice] = useState("");
  function saved(project: Project) {
    const created = dialog === "new";
    setNotice(
      created
        ? "Project created. A good start."
        : "Project updated. Keep it moving.",
    );
    onSaved?.(project, created);
    void mutate(isProjectKey);
  }
  return { dialog, setDialog, notice, setNotice, saved };
}
// SWR keys are [path, userId]; project paths start with /api/projects.
function isProjectKey(key: unknown): boolean {
  return (
    Array.isArray(key) &&
    typeof key[0] === "string" &&
    key[0].startsWith(apiRoutes.projects)
  );
}
