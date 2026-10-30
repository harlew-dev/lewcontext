import { Notice } from "@/components/ui/feedback";
import type { ProjectsState } from "./use-projects";
// Mutation success and load-failure messages for project pages.
export function ProjectNotices({ state }: { state: ProjectsState }) {
  return (
    <>
      {state.notice && (
        <Notice
          variant="success"
          className="mt-6"
          onDismiss={() => state.setNotice("")}
        >
          {state.notice}
        </Notice>
      )}
      {state.error && (
        <Notice variant="error" className="mt-6" onRetry={state.refresh}>
          {state.error}
        </Notice>
      )}
    </>
  );
}
