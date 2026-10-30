import {
  createContext,
  useContext,
  useId,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from "react";
import { Modal } from "baseui/modal";
import { slot } from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { useScrollLock } from "@/lib/scroll-lock";
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
// BaseUI closes the modal on clicks that land on DialogContainer or an ancestor,
// so the backdrop is the Root and the centring grid is the DialogContainer.
const DialogBackdrop = slot(
  "div",
  "fixed inset-0 w-screen overflow-y-auto bg-zinc-950/25 pt-6 transition duration-150 ease-out focus:outline-0 data-closed:pointer-events-none data-closed:opacity-0 data-closed:ease-in motion-reduce:transition-none sm:pt-0 dark:bg-zinc-950/50",
);
const DialogGrid = slot(
  "div",
  "grid min-h-full grid-rows-[1fr_auto] justify-items-center sm:grid-rows-[1fr_auto_3fr] sm:p-4",
);
const DialogPanel = slot(
  "div",
  [
    "row-start-2 w-full min-w-0 rounded-t-3xl bg-white p-(--gutter) shadow-lg ring-1 ring-zinc-950/10 [--gutter:--spacing(8)] sm:mb-auto sm:rounded-2xl dark:bg-zinc-900 dark:ring-white/10 forced-colors:outline",
    "transition duration-150 ease-out will-change-transform data-closed:translate-y-12 data-closed:opacity-0 data-closed:ease-in motion-reduce:transition-none sm:data-closed:translate-y-0 sm:data-closed:scale-95",
    "focus:outline-hidden",
  ].join(" "),
);
const NoCloseButton = () => null;
const DialogContext = createContext<{ titleId: string; descriptionId: string }>(
  { titleId: "", descriptionId: "" },
);
type DialogProps = {
  open: boolean;
  onClose: (value: false) => void;
  size?: keyof typeof sizes;
  className?: string;
  children: ReactNode;
};
// Dialog on BaseUI's Modal (focus lock, Escape, backdrop click,
// enter/leave transitions via data-closed). useScrollLock hides the page's
// scrollbar without shifting the layout.
export function Dialog({
  open,
  onClose,
  size = "lg",
  className,
  children,
}: DialogProps) {
  const id = useId();
  useScrollLock(open);
  const ids = { titleId: `${id}-title`, descriptionId: `${id}-description` };
  return (
    <Modal
      isOpen={open}
      onClose={() => onClose(false)}
      closeable
      animate
      autoFocus
      focusLock
      returnFocus
      role="dialog"
      overrides={{
        Root: { component: DialogBackdrop },
        DialogContainer: { component: DialogGrid },
        Dialog: {
          component: DialogPanel,
          props: {
            className: cx(sizes[size], className),
            "aria-label": undefined,
            "aria-labelledby": ids.titleId,
          },
        },
        Close: { component: NoCloseButton },
      }}
    >
      <DialogContext.Provider value={ids}>{children}</DialogContext.Provider>
    </Modal>
  );
}
export function DialogTitle({
  className,
  ...props
}: ComponentPropsWithoutRef<"h2">) {
  const { titleId } = useContext(DialogContext);
  return (
    <h2
      id={titleId}
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
  const { descriptionId } = useContext(DialogContext);
  return (
    <Text
      id={descriptionId}
      {...props}
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
