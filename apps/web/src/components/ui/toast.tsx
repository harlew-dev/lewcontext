import type { ReactNode } from "react";
import { ToasterContainer, toaster } from "baseui/toast";
import { slot } from "@/lib/baseui";
import { cx } from "@/lib/cx";
import {
  DismissButton,
  messageVariants,
  type MessageVariant,
} from "./feedback";
export type ToastOptions = {
  // Milliseconds before the toast hides itself; Infinity keeps it until
  // it's dismissed.
  duration?: number;
};
const defaultDuration = 5000;
// The stack: across the bottom on phones, bottom-right from sm. BaseUI renders
// the newest toast first, and the reversed column puts it nearest the edge.
const ToasterRoot = slot(
  "div",
  "pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col-reverse gap-3 p-4 sm:left-auto sm:w-96 sm:p-6",
);
// One toast. BaseUI reports it closed ($isVisible false) as it enters and
// leaves, so slot() marks both with data-closed.
const ToastPanel = slot(
  "div",
  [
    "pointer-events-auto rounded-xl bg-white shadow-lg ring-1 ring-zinc-950/10 dark:bg-zinc-900 dark:ring-white/10 forced-colors:outline",
    "transition duration-200 ease-out data-closed:pointer-events-none data-closed:translate-y-2 data-closed:opacity-0 data-closed:ease-in motion-reduce:transition-none",
  ].join(" "),
);
const ToastInner = slot("div", "");
function ToastMessage({
  variant,
  onDismiss,
  children,
}: {
  variant: MessageVariant;
  onDismiss: () => void;
  children: ReactNode;
}) {
  const { icon: Icon, iconColor } = messageVariants[variant];
  return (
    <div
      className={cx(
        "flex items-center gap-3 py-3 pr-2 pl-4 text-sm/6 text-zinc-950 dark:text-white",
        "*:data-[slot=icon]:size-5 *:data-[slot=icon]:shrink-0",
        iconColor,
      )}
    >
      <Icon />
      <span className="flex-1">{children}</span>
      <DismissButton onClick={onDismiss} />
    </div>
  );
}
// Where toasts appear. Mounted by the signed-in pages, so signing out takes
// any open toasts with it.
export function Toaster() {
  return (
    <ToasterContainer
      overrides={{
        Root: { component: ToasterRoot },
        ToastBody: { component: ToastPanel },
        ToastInnerContainer: { component: ToastInner },
      }}
    />
  );
}
// Toasts report finished actions; warnings and information stay inline.
type ToastVariant = Extract<MessageVariant, "success" | "error">;
let count = 0;
// BaseUI keeps the timer (paused while hovered or focused) and the stack. Its
// own close icon can't be reached by keyboard, so ours replaces it.
function show(
  variant: ToastVariant,
  message: ReactNode,
  { duration = defaultDuration }: ToastOptions = {},
) {
  const key = `toast-${++count}`;
  toaster.show(
    <ToastMessage variant={variant} onDismiss={() => toaster.clear(key)}>
      {message}
    </ToastMessage>,
    {
      key,
      role: messageVariants[variant].role,
      closeable: false,
      autoHideDuration: Number.isFinite(duration) ? duration : 0,
    },
  );
}
// A short-lived message for a finished action: `toast.success("Saved.")`, or
// `toast.error(message, { duration: Infinity })` to keep it until dismissed.
export const toast = {
  success: (message: ReactNode, options?: ToastOptions) =>
    show("success", message, options),
  error: (message: ReactNode, options?: ToastOptions) =>
    show("error", message, options),
};
