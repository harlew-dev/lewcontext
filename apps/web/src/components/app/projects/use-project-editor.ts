import { useState } from "react";
import { useRouter } from "next/router";
import useSWR from "swr";
import { ApiError, request } from "@/lib/api";
import { apiRoutes, routes } from "@/lib/routes";
import type { Project, WorkspaceProps } from "@/lib/types";
export type ProjectsState = ReturnType<typeof useProjects>;
export function useProjects(props: WorkspaceProps) {
  const router = useRouter();
  const [dialog, setDialog] = useState<Project | "new" | null>(null);
  const [notice, setNotice] = useState("");
  const { data, error, isLoading, isValidating, mutate } = useSWR<Project[]>(
    [apiRoutes.projects, props.session.user.id],
    ([path]: [string, number]) => request<Project[]>(path),
    {
      fallbackData: props.initialProjects ?? undefined,
      onError: (error) => {
        if (error instanceof ApiError && error.status === 401)
          void router.replace(routes.login);
      },
    },
  );
  async function saved(project: Project) {
    await mutate(
      (current) => [
        project,
        ...(current || []).filter((p) => p.id !== project.id),
      ],
      { revalidate: false },
    );
    setNotice(
      dialog === "new"
        ? "Project created. A good start."
        : "Project updated. Keep it moving.",
    );
    void mutate();
  }
  return {
    projects: data || [],
    error: error ? error.message : !data ? props.initialError : null,
    loading: isLoading && !data,
    refreshing: isValidating,
    dialog,
    setDialog,
    notice,
    setNotice,
    saved,
    refresh: () => void mutate(),
  };
}
