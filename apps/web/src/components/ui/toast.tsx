import type { ReactNode } from "react";
import { Toast } from "@base-ui/react/toast";
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
// Toasts report finished actions; warnings and information stay inline.
type ToastVariant = Extract<MessageVariant, "success" | "error">;
type ToastData = { variant: ToastVariant; message: ReactNode };
// The one manager `toast` calls and the Toaster listens to.
const manager = Toast.createToastManager();
// Where toasts appear: across the bottom on phones, bottom-right from sm.
// They stack with the newest in front, the older ones peeking out behind it;
// hovering or focusing the stack fans it out and pauses the timers.
const viewportClasses =
  "fixed right-4 bottom-4 left-4 z-[80] sm:left-auto sm:right-6 sm:bottom-6 sm:w-84";
// One toast. The stack's position, scale and height come from the variables
// Base UI sets on each toast (`--toast-index`, `--toast-offset-y`…).
const rootClasses = [
  "[--gap:0.75rem] [--peek:0.75rem] [--scale:calc(max(0,1-(var(--toast-index)*0.1)))] [--shrink:calc(1-var(--scale))] [--height:var(--toast-frontmost-height,var(--toast-height))] [--offset-y:calc(var(--toast-offset-y)*-1+calc(var(--toast-index)*var(--gap)*-1)+var(--toast-swipe-movement-y))]",
  "absolute right-0 bottom-0 left-auto z-[calc(1000-var(--toast-index))] h-(--height) w-full origin-bottom select-none",
  "[transform:translateX(var(--toast-swipe-movement-x))_translateY(calc(var(--toast-swipe-movement-y)-(var(--toast-index)*var(--peek))-(var(--shrink)*var(--height))))_scale(var(--scale))]",
  "rounded-xl bg-white shadow-lg ring-1 ring-zinc-950/10 dark:bg-zinc-900 dark:ring-white/10 forced-colors:outline",
  "transition-[transform,opacity,height] duration-500 ease-[cubic-bezier(0.22,1,0.36,1)] motion-reduce:transition-none",
  // The gap between toasts belongs to the stack, so the pointer doesn't leave it.
  "after:absolute after:top-full after:left-0 after:h-[calc(var(--gap)+1px)] after:w-full after:content-['']",
  "data-expanded:h-(--toast-height) data-expanded:[transform:translateX(var(--toast-swipe-movement-x))_translateY(var(--offset-y))]",
  "data-starting-style:[transform:translateY(150%)]",
  "data-ending-style:opacity-0 [&[data-ending-style]:not([data-limited])]:[transform:translateY(150%)]",
  "data-limited:opacity-0",
].join(" ");
function ToastMessage({
  variant,
  onDismiss,
  children,
}: {
  variant: ToastVariant;
  onDismiss: () => void;
  children: ReactNode;
}) {
  const { icon: Icon, role, iconColor } = messageVariants[variant];
  return (
    <Toast.Content
      className={cx(
        "overflow-hidden transition-opacity duration-250 ease-out data-behind:opacity-0 data-expanded:opacity-100 motion-reduce:transition-none",
      )}
    >
      <div
        role={role}
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
    </Toast.Content>
  );
}
function ToastList() {
  const { toasts, close } = Toast.useToastManager();
  return toasts.map((item) => {
    const { variant, message } = item.data as ToastData;
    return (
      <Toast.Root
        key={item.id}
        toast={item}
        swipeDirection={[]}
        // Base UI makes each toast a dialog; the message inside already has
        // its own status or alert role, and a dialog would also answer a
        // search for the page's real ones.
        role="presentation"
        className={rootClasses}
      >
        <ToastMessage variant={variant} onDismiss={() => close(item.id)}>
          {message}
        </ToastMessage>
      </Toast.Root>
    );
  });
}
// Where toasts appear. Mounted by the signed-in pages, so signing out takes
// any open toasts with it.
export function Toaster() {
  return (
    <Toast.Provider toastManager={manager} limit={5}>
      <Toast.Portal>
        <Toast.Viewport className={viewportClasses}>
          <ToastList />
        </Toast.Viewport>
      </Toast.Portal>
    </Toast.Provider>
  );
}
function show(
  variant: ToastVariant,
  message: ReactNode,
  { duration = defaultDuration }: ToastOptions = {},
) {
  manager.add<ToastData>({
    data: { variant, message },
    // 0 keeps it until dismissed.
    timeout: Number.isFinite(duration) ? duration : 0,
  });
}
// A short-lived message for a finished action: `toast.success("Saved.")`, or
// `toast.error(message, { duration: Infinity })` to keep it until dismissed.
export const toast = {
  success: (message: ReactNode, options?: ToastOptions) =>
    show("success", message, options),
  error: (message: ReactNode, options?: ToastOptions) =>
    show("error", message, options),
};
