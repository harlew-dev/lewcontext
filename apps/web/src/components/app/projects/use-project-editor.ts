import { useState } from "react";
import { useSWRConfig } from "swr";
import { paths } from "@/lib/api";
import { useWorkspace } from "@/lib/workspace";
import type { Project } from "@/types/projects";
export type ProjectEditor = ReturnType<typeof useProjectEditor>;
// Create/edit dialog state and success notice for project pages. After a save
// every cached list and project of this workspace revalidates, since the
// change can move it between pages, filters and counts.
export function useProjectEditor(
  onSaved?: (project: Project, created: boolean) => void,
) {
  const { mutate } = useSWRConfig();
  const projects = paths.projects(useWorkspace().id);
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
    // SWR keys are [path, userId].
    void mutate(
      (key) =>
        Array.isArray(key) &&
        typeof key[0] === "string" &&
        projects.matches(key[0]),
    );
  }
  return { dialog, setDialog, notice, setNotice, saved };
}
