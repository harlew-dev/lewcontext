import { Notice } from "@/components/ui/feedback";
// A project page's load failure, with a retry.
export function ProjectNotices({
  error,
  onRetry,
}: {
  error: string | null;
  onRetry: () => void;
}) {
  if (!error) return null;
  return (
    <Notice variant="error" className="mt-6" onRetry={onRetry}>
      {error}
    </Notice>
  );
}
