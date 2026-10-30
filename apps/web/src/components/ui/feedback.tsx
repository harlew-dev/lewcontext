import type { ReactNode } from "react";
import {
  CheckCircleIcon,
  XCircleIcon,
  XMarkIcon,
} from "@heroicons/react/20/solid";
import { FolderPlusIcon } from "@heroicons/react/24/outline";
import { cx } from "@/lib/cx";
import { Button } from "./button";
import { Subheading } from "./heading";
import { Text } from "./text";
// How each kind of message is marked and announced: errors as alerts,
// successes as polite status updates. Notice and toast share these.
export const messageVariants = {
  error: {
    icon: XCircleIcon,
    role: "alert",
    iconColor: "*:data-[slot=icon]:fill-red-500",
  },
  success: {
    icon: CheckCircleIcon,
    role: "status",
    iconColor: "*:data-[slot=icon]:fill-lime-600",
  },
} as const;
export type MessageVariant = keyof typeof messageVariants;
const noticeColors: Record<MessageVariant, string> = {
  error:
    "bg-red-50 text-red-800 ring-red-600/10 dark:bg-red-500/10 dark:text-red-300 dark:ring-red-500/20",
  success:
    "bg-lime-50 text-lime-800 ring-lime-600/15 dark:bg-lime-400/10 dark:text-lime-300 dark:ring-lime-400/20",
};
// The close button on a Notice or toast.
export function DismissButton({ onClick }: { onClick: () => void }) {
  return (
    <Button plain aria-label="Dismiss notification" onClick={onClick}>
      <XMarkIcon />
    </Button>
  );
}
// Inline message, next to the form or list it belongs to.
export function Notice({
  variant,
  children,
  onRetry,
  onDismiss,
  className,
}: {
  variant: MessageVariant;
  children: ReactNode;
  onRetry?: () => void;
  onDismiss?: () => void;
  className?: string;
}) {
  const { icon: Icon, role, iconColor } = messageVariants[variant];
  return (
    <div
      role={role}
      className={cx(
        className,
        "flex items-center gap-3 rounded-lg px-4 py-3 text-sm/6 ring-1 ring-inset",
        "*:data-[slot=icon]:size-5 *:data-[slot=icon]:shrink-0",
        noticeColors[variant],
        iconColor,
      )}
    >
      <Icon />
      <span className="flex-1">{children}</span>
      {onRetry && (
        <Button plain onClick={onRetry}>
          Try again
        </Button>
      )}
      {onDismiss && <DismissButton onClick={onDismiss} />}
    </div>
  );
}
export function EmptyState({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center px-4 py-12 text-center">
      <FolderPlusIcon
        className="size-10 stroke-zinc-400 dark:stroke-zinc-500"
        aria-hidden="true"
      />
      <Subheading className="mt-4">{title}</Subheading>
      <Text className="mt-1">{description}</Text>
      {action && <div className="mt-6">{action}</div>}
    </div>
  );
}
// Placeholder rows while a list loads.
export function ListSkeleton({
  label,
  rows = 3,
}: {
  label: string;
  rows?: number;
}) {
  return (
    <div
      role="status"
      aria-label={label}
      className="divide-y divide-zinc-950/5 dark:divide-white/5"
    >
      {Array.from({ length: rows }, (_, row) => (
        <div key={row} className="flex animate-pulse items-center gap-4 py-4">
          <span className="size-8 rounded-md bg-zinc-950/5 dark:bg-white/5" />
          <span className="h-4 flex-1 rounded bg-zinc-950/5 dark:bg-white/5" />
          <span className="h-4 w-20 rounded bg-zinc-950/5 dark:bg-white/5" />
        </div>
      ))}
    </div>
  );
}
