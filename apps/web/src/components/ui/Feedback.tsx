import type { ReactNode } from "react";
import { AlertCircle, CheckCircle2, FolderPlus, X } from "lucide-react";
import { Spinner } from "baseui/spinner";
import { Button } from "./Button";
import { IconButton } from "./IconButton";
import { Eyebrow } from "./Typography";
import { cx } from "@/lib/cx";
const notices = {
  error: {
    className: "border border-danger-line bg-danger-soft text-danger",
    icon: AlertCircle,
    role: "alert",
  },
  success: {
    className: "bg-success-soft text-success",
    icon: CheckCircle2,
    role: "status",
  },
} as const;
// Inline message. Errors announce as alerts; successes as polite status updates.
export function Notice({
  variant,
  children,
  onRetry,
  onDismiss,
  className,
}: {
  variant: keyof typeof notices;
  children: ReactNode;
  onRetry?: () => void;
  onDismiss?: () => void;
  className?: string;
}) {
  const { icon: Icon, role, className: variantClass } = notices[variant];
  return (
    <div
      role={role}
      className={cx(
        "mb-4.5 flex items-center gap-2.25 rounded-[7px] px-3.5 py-3 text-small",
        variantClass,
        className,
      )}
    >
      <Icon size={16} className="shrink-0" />
      <span>{children}</span>
      {onRetry && (
        <Button kind="tertiary" onClick={onRetry}>
          Try again
        </Button>
      )}
      {onDismiss && (
        <IconButton
          className="ml-auto"
          aria-label="Dismiss notification"
          onClick={onDismiss}
        >
          <X size={14} />
        </IconButton>
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
    <div
      className="flex items-center justify-center gap-3 p-8.75 text-body text-mauve-600"
      role="status"
    >
      <Spinner $size="20px" />
      <span>{label}</span>
    </div>
  );
}
export function ProjectSkeleton() {
  return (
    <div role="status" aria-label="Loading projects">
      {[1, 2, 3].map((i) => (
        <div className="flex gap-5 border-b border-[#eee] py-5" key={i}>
          <span className="skeleton-shimmer h-6.25 w-3/5 rounded-[5px]" />
          <span className="skeleton-shimmer h-6.25 w-1/5 rounded-[5px]" />
          <span className="skeleton-shimmer h-6.25 w-1/5 rounded-[5px]" />
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
    <div className="flex flex-col items-center px-3.75 py-13.75 text-center text-mauve-500">
      <FolderPlus size={28} />
      <h3 className="mt-3.75 text-[#665a71]">
        {filtered ? "No matching projects" : "Room for your next big thing"}
      </h3>
      <p className="mt-2 mb-5 text-small">
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
// Full-page message used for the service outage and 404 pages.
export function StatusPage({
  eyebrow,
  title,
  children,
}: {
  eyebrow?: string;
  title: string;
  children: ReactNode;
}) {
  return (
    <main className="mx-auto my-[20vh] max-w-150 p-7.5 [&>p]:my-3.75 [&>p]:text-muted">
      {eyebrow && <Eyebrow className="mb-2.75">{eyebrow}</Eyebrow>}
      <h1>{title}</h1>
      {children}
    </main>
  );
}
export function ServiceUnavailable() {
  return (
    <StatusPage title="A brief pause.">
      <p>We’re having trouble reaching your workspace.</p>
      <p>Your data is safe. Please try again in a moment.</p>
      <Button onClick={() => window.location.reload()}>Try again</Button>
    </StatusPage>
  );
}
