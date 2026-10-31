import {
  forwardRef,
  type ComponentPropsWithoutRef,
  type FocusEventHandler,
  type ReactNode,
} from "react";
import {
  EyeIcon,
  EyeSlashIcon,
  LockClosedIcon,
} from "@heroicons/react/16/solid";
import {
  Input as BaseInput,
  type InputProps as BaseInputProps,
} from "baseui/input";
import { slot } from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { useInteractive } from "@/lib/interactive";
import { useField } from "./fieldset";
import { Tooltip } from "./tooltip";
// The bordered control wrapper shared with Textarea (data-slot="control").
export const controlClasses = [
  "relative block w-full",
  "before:absolute before:inset-px before:rounded-[calc(var(--radius-lg)-1px)] before:bg-white before:shadow-sm",
  "dark:before:hidden",
  "after:pointer-events-none after:absolute after:inset-0 after:rounded-lg after:ring-transparent after:ring-inset sm:focus-within:after:ring-2 sm:focus-within:after:ring-blue-500",
  "has-data-disabled:opacity-50 has-data-disabled:before:bg-zinc-950/5 has-data-disabled:before:shadow-none",
].join(" ");
// Text field styles shared with Textarea. BaseUI does not report hover, so the
// hover states use `hover:` rather than `data-hover:`.
export const fieldClasses = [
  "relative block w-full appearance-none rounded-lg px-[calc(--spacing(3.5)-1px)] py-[calc(--spacing(2.5)-1px)] sm:px-[calc(--spacing(3)-1px)] sm:py-[calc(--spacing(1.5)-1px)]",
  "text-base/6 text-zinc-950 placeholder:text-zinc-500 sm:text-sm/6 dark:text-white",
  "border border-zinc-950/10 hover:border-zinc-950/20 dark:border-white/10 dark:hover:border-white/20",
  "bg-transparent dark:bg-white/5",
  "focus:outline-hidden",
  "data-invalid:border-red-500 data-invalid:hover:border-red-500 dark:data-invalid:border-red-600 dark:data-invalid:hover:border-red-600",
  "data-disabled:border-zinc-950/20 dark:data-disabled:border-white/15 dark:data-disabled:bg-white/2.5 dark:hover:data-disabled:border-white/15",
].join(" ");
const InputControl = slot("span", controlClasses, {
  props: () => ({ "data-slot": "control" }),
});
const InputContainer = slot("span", "contents");
const InputElement = slot("input", cx(fieldClasses, "dark:scheme-dark"));
const MaskToggle = slot(
  "button",
  "absolute inset-y-0 right-0 z-10 flex items-center px-3 text-zinc-500 hover:text-zinc-700 focus:outline-hidden data-focus:text-zinc-950 sm:px-2.5 dark:text-zinc-400 dark:hover:text-zinc-200 dark:data-focus:text-white",
);
const ShowIcon = () => <EyeIcon className="size-5 sm:size-4" />;
const HideIcon = () => <EyeSlashIcon className="size-5 sm:size-4" />;
export type InputProps = Omit<
  BaseInputProps,
  "overrides" | "size" | "error" | "inputRef" | "startEnhancer" | "endEnhancer"
> & {
  invalid?: boolean;
  minLength?: number;
  className?: string;
};
// Input on top of BaseUI's Input. Inside a <Field> it takes the field's
// id, description and disabled state.
export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { invalid = false, minLength, className, type, ...props },
  ref,
) {
  const field = useField();
  return (
    <BaseInput
      id={field?.id}
      aria-describedby={field?.describedBy}
      disabled={field?.disabled}
      {...props}
      type={type}
      error={invalid}
      inputRef={ref as React.RefObject<HTMLInputElement>}
      overrides={{
        Root: { component: InputControl, props: { className } },
        InputContainer: { component: InputContainer },
        Input: {
          component: InputElement,
          props: {
            minLength,
            className: type === "password" ? "pr-10 sm:pr-9" : undefined,
          },
        },
        MaskToggleButton: { component: MaskToggle },
        MaskToggleShowIcon: { component: ShowIcon },
        MaskToggleHideIcon: { component: HideIcon },
      }}
    />
  );
});
// Positions a leading/trailing icon inside an Input, or InputActions at its
// end. The input's right padding makes room for one or two actions.
export function InputGroup({ children }: { children: ReactNode }) {
  return (
    <span
      data-slot="control"
      className={cx(
        "relative isolate block",
        "has-[>[data-slot=icon]:first-child]:[&_input]:pl-10 has-[>[data-slot=icon]:last-child]:[&_input]:pr-10 sm:has-[>[data-slot=icon]:first-child]:[&_input]:pl-8 sm:has-[>[data-slot=icon]:last-child]:[&_input]:pr-8",
        "has-[>[data-slot=actions]]:[&_input]:pr-10 sm:has-[>[data-slot=actions]]:[&_input]:pr-9",
        "has-[>[data-slot=actions]>:nth-child(2)]:[&_input]:pr-18 sm:has-[>[data-slot=actions]>:nth-child(2)]:[&_input]:pr-16",
        "*:data-[slot=icon]:pointer-events-none *:data-[slot=icon]:absolute *:data-[slot=icon]:top-3 *:data-[slot=icon]:z-10 *:data-[slot=icon]:size-5 sm:*:data-[slot=icon]:top-2.5 sm:*:data-[slot=icon]:size-4",
        "[&>[data-slot=icon]:first-child]:left-3 sm:[&>[data-slot=icon]:first-child]:left-2.5 [&>[data-slot=icon]:last-child]:right-3 sm:[&>[data-slot=icon]:last-child]:right-2.5",
        "*:data-[slot=icon]:text-zinc-500 dark:*:data-[slot=icon]:text-zinc-400",
      )}
    >
      {children}
    </span>
  );
}
// Controls at the end of an Input in an InputGroup: InputLock, InputButton.
export function InputActions({ children }: { children: ReactNode }) {
  return (
    <span
      data-slot="actions"
      className="absolute inset-y-0 right-1 z-10 flex items-center"
    >
      {children}
    </span>
  );
}
const actionClasses = [
  "flex size-8 items-center justify-center rounded-md sm:size-7",
  "text-zinc-500 dark:text-zinc-400",
  "*:data-[slot=icon]:size-5 sm:*:data-[slot=icon]:size-4",
  "focus:not-data-focus:outline-hidden data-focus:outline-2 data-focus:outline-blue-500",
].join(" ");
type ActionProps<T> = Pick<
  ComponentPropsWithoutRef<"span">,
  "onMouseEnter" | "onMouseLeave" | "id" | "aria-describedby"
> & {
  onFocus?: FocusEventHandler<T>;
  onBlur?: FocusEventHandler<T>;
};
// The lock trigger; Tooltip passes it the hover and focus handlers.
const LockIcon = forwardRef<
  HTMLSpanElement,
  ActionProps<HTMLSpanElement> & { label: string }
>(function LockIcon({ label, onFocus, onBlur, ...props }, ref) {
  const interactive = useInteractive<HTMLSpanElement>({ onFocus, onBlur });
  return (
    <span
      ref={ref}
      role="img"
      aria-label={label}
      tabIndex={0}
      {...props}
      {...interactive}
      className={actionClasses}
    >
      <LockClosedIcon />
    </span>
  );
});
// Marks a read-only Input whose value can't be changed. `label` says why, in
// the tooltip and as the icon's accessible name. Focusable, so the tooltip
// also opens from the keyboard.
export function InputLock({ label = "Can’t be changed" }: { label?: string }) {
  return (
    <Tooltip content={label}>
      <LockIcon label={label} />
    </Tooltip>
  );
}
export type InputButtonProps = ActionProps<HTMLButtonElement> & {
  "aria-label": string;
  onClick: () => void;
  children: ReactNode;
};
// An icon button inside an Input, e.g. Copy. Wrap it in a Tooltip naming it.
export const InputButton = forwardRef<HTMLButtonElement, InputButtonProps>(
  function InputButton({ onFocus, onBlur, children, ...props }, ref) {
    const interactive = useInteractive<HTMLButtonElement>({ onFocus, onBlur });
    return (
      <button
        ref={ref}
        type="button"
        {...props}
        {...interactive}
        className={cx(
          actionClasses,
          "data-active:bg-zinc-950/5 data-hover:bg-zinc-950/5 data-hover:text-zinc-700",
          "dark:data-active:bg-white/10 dark:data-hover:bg-white/10 dark:data-hover:text-zinc-300",
        )}
      >
        {children}
      </button>
    );
  },
);
