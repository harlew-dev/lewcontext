import { useState, type ComponentPropsWithoutRef, type ReactNode } from "react";
import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import { cx } from "@/lib/cx";
import { ReturnFocus, focusedElement } from "@/lib/return-focus";
import { Text } from "./text";
const sizes = {
  xs: "sm:max-w-xs",
  sm: "sm:max-w-sm",
  md: "sm:max-w-md",
  lg: "sm:max-w-lg",
  xl: "sm:max-w-xl",
  "2xl": "sm:max-w-2xl",
  "3xl": "sm:max-w-3xl",
  "4xl": "sm:max-w-4xl",
  "5xl": "sm:max-w-5xl",
};
// The backdrop dims the page and fades with the dialog. `starting:` (CSS
// @starting-style) animates one that mounts already open (a dialog keyed per
// opening, useOpenCount), which Base UI doesn't give data-starting-style.
const backdropClasses =
  "fixed inset-0 z-50 bg-zinc-950/25 transition duration-150 ease-out starting:opacity-0 data-starting-style:opacity-0 data-ending-style:opacity-0 data-ending-style:ease-in motion-reduce:transition-none dark:bg-zinc-950/50";
// The viewport scrolls a tall dialog; the grid centres the panel (a sheet at
// the bottom on phones).
const viewportClasses =
  "fixed inset-0 z-50 w-screen overflow-y-auto pt-6 focus:outline-0 sm:pt-0";
const gridClasses =
  "grid min-h-full grid-rows-[1fr_auto] justify-items-center sm:grid-rows-[1fr_auto_3fr] sm:p-4";
const panelClasses = [
  "row-start-2 w-full min-w-0 rounded-t-3xl bg-white p-(--gutter) shadow-lg ring-1 ring-zinc-950/10 [--gutter:--spacing(8)] sm:mb-auto sm:rounded-2xl dark:bg-zinc-900 dark:ring-white/10 forced-colors:outline",
  "transition duration-150 ease-out will-change-transform starting:translate-y-12 starting:opacity-0 data-starting-style:translate-y-12 data-starting-style:opacity-0 data-ending-style:translate-y-12 data-ending-style:opacity-0 data-ending-style:ease-in motion-reduce:transition-none sm:starting:translate-y-0 sm:starting:scale-95 sm:data-starting-style:translate-y-0 sm:data-starting-style:scale-95 sm:data-ending-style:translate-y-0 sm:data-ending-style:scale-95",
  "focus:outline-hidden",
].join(" ");
type DialogProps = {
  open: boolean;
  onClose: (value: false) => void;
  size?: keyof typeof sizes;
  className?: string;
  children: ReactNode;
};
// The element focused when the dialog opened, read while rendering, before
// the dialog moves focus into itself.
function useOpener(open: boolean) {
  // A dialog can mount open (e.g. keyed to reset its form each time).
  const [opener, setOpener] = useState(() => (open ? focusedElement() : null));
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setOpener(focusedElement());
  }
  return opener;
}
// Dialog on Base UI's Dialog (focus trap, Escape, backdrop click, scroll
// lock, enter and exit transitions on data-starting-style and
// data-ending-style). Its title and description name it (aria-labelledby and
// aria-describedby). Focus goes back to whatever had it when the dialog
// opened, which Base UI loses for a dialog opened from a menu item.
export function Dialog({
  open,
  onClose,
  size = "lg",
  className,
  children,
}: DialogProps) {
  const opener = useOpener(open);
  return (
    <BaseDialog.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose(false);
      }}
    >
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className={backdropClasses} />
        <BaseDialog.Viewport className={viewportClasses}>
          <div className={gridClasses}>
            <BaseDialog.Popup
              finalFocus={false}
              className={cx(panelClasses, sizes[size], className)}
            >
              {children}
              <ReturnFocus opener={opener} />
            </BaseDialog.Popup>
          </div>
        </BaseDialog.Viewport>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  );
}
export function DialogTitle({
  className,
  ...props
}: ComponentPropsWithoutRef<"h2">) {
  return (
    <BaseDialog.Title
      {...props}
      className={cx(
        className,
        "text-lg/6 font-semibold text-balance text-zinc-950 sm:text-base/6 dark:text-white",
      )}
    />
  );
}
export function DialogDescription({
  className,
  ...props
}: ComponentPropsWithoutRef<"p">) {
  return (
    <BaseDialog.Description
      {...props}
      render={<Text />}
      className={cx(className, "mt-2 text-pretty")}
    />
  );
}
export function DialogBody({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return <div {...props} className={cx(className, "mt-6")} />;
}
export function DialogActions({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      {...props}
      className={cx(
        className,
        "mt-8 flex flex-col-reverse items-center justify-end gap-3 *:w-full sm:flex-row sm:*:w-auto",
      )}
    />
  );
}
