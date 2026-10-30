import { Notice } from "@/components/ui/feedback";
import type { ProjectEditor } from "./use-project-editor";
// Mutation success and load-failure messages for project pages.
export function ProjectNotices({
  editor,
  error,
  onRetry,
}: {
  editor: ProjectEditor;
  error: string | null;
  onRetry: () => void;
}) {
  return (
    <>
      {editor.notice && (
        <Notice
          variant="success"
          className="mt-6"
          onDismiss={() => editor.setNotice("")}
        >
          {editor.notice}
        </Notice>
      )}
      {error && (
        <Notice variant="error" className="mt-6" onRetry={onRetry}>
          {error}
        </Notice>
      )}
    </>
  );
}
