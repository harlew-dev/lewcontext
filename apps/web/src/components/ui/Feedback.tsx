import { AlertCircle, FolderPlus } from "lucide-react";
import { Spinner } from "baseui/spinner";
import { Button } from "./Button";
export function ErrorNotice({
  message,
  retry,
}: {
  message: string;
  retry?: () => void;
}) {
  return (
    <div className="error-notice" role="alert">
      <AlertCircle size={17} />
      <span>{message}</span>
      {retry && (
        <Button kind="tertiary" onClick={retry}>
          Try again
        </Button>
      )}
    </div>
  );
}
export function Loading({
  label = "Loading your workspace…",
}: {
  label?: string;
}) {
  return (
    <div className="loading" role="status">
      <Spinner $size="20px" />
      <span>{label}</span>
    </div>
  );
}
export function ProjectSkeleton() {
  return (
    <div className="skeleton-list" role="status" aria-label="Loading projects">
      {[1, 2, 3].map((i) => (
        <div className="skeleton-row" key={i}>
          <span />
          <span />
          <span />
        </div>
      ))}
    </div>
  );
}
export function EmptyState({
  filtered,
  onCreate,
}: {
  filtered?: boolean;
  onCreate?: () => void;
}) {
  return (
    <div className="empty-state">
      <FolderPlus size={28} />
      <h3>
        {filtered ? "No matching projects" : "Room for your next big thing"}
      </h3>
      <p>
        {filtered
          ? "Try another search or a different status."
          : "Create your first project and give your work a place to grow."}
      </p>
      {onCreate && !filtered && (
        <Button onClick={onCreate}>Create a project</Button>
      )}
    </div>
  );
}
export function ServiceUnavailable() {
  return (
    <main className="service-unavailable">
      <h1>A brief pause.</h1>
      <p>We’re having trouble reaching your workspace.</p>
      <p>Your data is safe. Please try again in a moment.</p>
      <Button onClick={() => window.location.reload()}>Try again</Button>
    </main>
  );
}
