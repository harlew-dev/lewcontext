import {
  createContext,
  forwardRef,
  useContext,
  type ComponentPropsWithoutRef,
  type FocusEventHandler,
  type ReactNode,
} from "react";
import {
  EyeIcon,
  EyeSlashIcon,
  LockClosedIcon,
  ShieldCheckIcon,
} from "@heroicons/react/16/solid";
import {
  Input as BaseInput,
  type InputProps as BaseInputProps,
} from "baseui/input";
import { slot } from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { useInteractive } from "@/lib/interactive";
import { useField, type FieldLock } from "./fieldset";
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
// The control with anything drawn over the input, e.g. its blurred text.
const InputRoot = forwardRef<
  HTMLSpanElement,
  { overlay?: ReactNode; children?: ReactNode }
>(function InputRoot({ overlay, children, ...props }, ref) {
  return (
    <InputControl ref={ref} {...props}>
      {children}
      {overlay}
    </InputControl>
  );
});
// Blurred, the input's own text is invisible and BlurredText draws it.
const InputElement = slot(
  "input",
  cx(
    fieldClasses,
    "data-blurred:text-transparent data-blurred:caret-transparent",
    "dark:scheme-dark",
  ),
);
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
  // Something sensitive but not secret, e.g. an email address: it's in the
  // page, blurred unreadable until shown. A secret stays out of the page
  // instead (a masked value, or Reveal). With `onReveal`, clicking anywhere
  // on it, or on its "Click to reveal" tooltip, calls that; keyboard users need
  // another way, such as an eye InputButton.
  blurred?: boolean;
  onReveal?: () => void;
  minLength?: number;
  className?: string;
};
// Keys that would change the value: a character, a deletion, paste or cut.
function isEdit(event: { key: string; ctrlKey: boolean; metaKey: boolean }) {
  if (event.key === "Backspace" || event.key === "Delete") return true;
  if (event.ctrlKey || event.metaKey) return /^[vx]$/i.test(event.key);
  return event.key.length === 1;
}
// Input on top of BaseUI's Input. Inside a <Field> it takes the field's
// id, description, disabled state and lock: a locked field's input is
// read-only, ends with the lock (in its InputGroup's InputActions, or its own
// when it has no group), and a try at typing, deleting, pasting or dropping
// shows the lock's tooltip. Escape or leaving the input hides it.
export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  {
    invalid = false,
    blurred = false,
    onReveal,
    minLength,
    className,
    type,
    readOnly,
    onKeyDown,
    onBlur,
    ...props
  },
  ref,
) {
  const field = useField();
  const lock = field?.lock;
  const readOnlyLock = lock?.readOnly ? lock : undefined;
  const grouped = useContext(InputGroupContext);
  const overlay = blurred ? (
    <BlurredText value={String(props.value ?? "")} onReveal={onReveal} />
  ) : null;
  const input = (
    <BaseInput
      id={field?.id}
      aria-describedby={field?.describedBy}
      disabled={field?.disabled}
      {...props}
      type={type}
      readOnly={readOnly || Boolean(readOnlyLock)}
      onKeyDown={(event) => {
        if (readOnlyLock && event.key === "Escape") readOnlyLock.hide();
        else if (readOnlyLock && isEdit(event)) readOnlyLock.show();
        onKeyDown?.(event);
      }}
      onBlur={(event) => {
        lock?.hide();
        onBlur?.(event);
      }}
      error={invalid}
      inputRef={ref as React.RefObject<HTMLInputElement>}
      overrides={{
        Root: { component: InputRoot, props: { className, overlay } },
        InputContainer: { component: InputContainer },
        Input: {
          component: InputElement,
          props: {
            minLength,
            onPaste: readOnlyLock?.show,
            onDrop: readOnlyLock?.show,
            "data-blurred": blurred ? "" : undefined,
            className: type === "password" ? "pr-10 sm:pr-9" : undefined,
          },
        },
        MaskToggleButton: { component: MaskToggle },
        MaskToggleShowIcon: { component: ShowIcon },
        MaskToggleHideIcon: { component: HideIcon },
      }}
    />
  );
  if (!lock || grouped) return input;
  return (
    <InputGroup>
      {input}
      <InputActions />
    </InputGroup>
  );
});
// The value drawn blurred over a blurred Input. It's outside the input, which
// would clip the blur to its line. Its padding matches the input's, InputGroup
// included. The input underneath keeps the value for its label and screen
// readers, so this is hidden from them.
function BlurredText({
  value,
  onReveal,
}: {
  value: string;
  onReveal?: () => void;
}) {
  const classes = cx(
    "absolute inset-0 flex items-center px-3.5 text-left sm:px-3",
    "text-base/6 text-zinc-950 sm:text-sm/6 dark:text-white",
  );
  const text = <span className="min-w-0 flex-1 truncate blur-xs">{value}</span>;
  if (!onReveal)
    return (
      <span
        data-slot="blurred"
        aria-hidden="true"
        className={cx(classes, "pointer-events-none")}
      >
        {text}
      </span>
    );
  return (
    <Tooltip content="Click to reveal" onClick={onReveal}>
      <button
        type="button"
        data-slot="blurred"
        tabIndex={-1}
        aria-hidden="true"
        onClick={onReveal}
        className={cx(classes, "cursor-pointer focus:outline-hidden")}
      >
        {text}
      </button>
    </Tooltip>
  );
}
// Tells an Input it's in an InputGroup, whose InputActions show its lock.
const InputGroupContext = createContext(false);
// Positions a leading/trailing icon inside an Input, or InputActions at its
// end. The input's right padding makes room for one or two actions. In a
// locked Field, the lock goes at the end of the InputActions, so a group with
// a lock needs them.
export function InputGroup({ children }: { children: ReactNode }) {
  return (
    <InputGroupContext.Provider value={true}>
      <span
        data-slot="control"
        className={cx(
          "relative isolate block",
          "has-[>[data-slot=icon]:first-child]:[&_:is(input,[data-slot=blurred])]:pl-10 has-[>[data-slot=icon]:last-child]:[&_:is(input,[data-slot=blurred])]:pr-10 sm:has-[>[data-slot=icon]:first-child]:[&_:is(input,[data-slot=blurred])]:pl-8 sm:has-[>[data-slot=icon]:last-child]:[&_:is(input,[data-slot=blurred])]:pr-8",
          "has-[>[data-slot=actions]]:[&_:is(input,[data-slot=blurred])]:pr-10 sm:has-[>[data-slot=actions]]:[&_:is(input,[data-slot=blurred])]:pr-9",
          "has-[>[data-slot=actions]>:nth-child(2)]:[&_:is(input,[data-slot=blurred])]:pr-18 sm:has-[>[data-slot=actions]>:nth-child(2)]:[&_:is(input,[data-slot=blurred])]:pr-16",
          "*:data-[slot=icon]:pointer-events-none *:data-[slot=icon]:absolute *:data-[slot=icon]:top-3 *:data-[slot=icon]:z-10 *:data-[slot=icon]:size-5 sm:*:data-[slot=icon]:top-2.5 sm:*:data-[slot=icon]:size-4",
          "[&>[data-slot=icon]:first-child]:left-3 sm:[&>[data-slot=icon]:first-child]:left-2.5 [&>[data-slot=icon]:last-child]:right-3 sm:[&>[data-slot=icon]:last-child]:right-2.5",
          "*:data-[slot=icon]:text-zinc-500 dark:*:data-[slot=icon]:text-zinc-400",
        )}
      >
        {children}
      </span>
    </InputGroupContext.Provider>
  );
}
// Controls at the end of an Input in an InputGroup: InputButtons, then a
// locked Field's lock.
export function InputActions({ children }: { children?: ReactNode }) {
  const lock = useField()?.lock;
  return (
    <span
      data-slot="actions"
      className="absolute inset-y-0 right-1 z-10 flex items-center"
    >
      {children}
      {lock && <InputLock lock={lock} />}
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
  ActionProps<HTMLSpanElement> & { label: string; shield: boolean }
>(function LockIcon({ label, shield, onFocus, onBlur, ...props }, ref) {
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
      {shield ? <ShieldCheckIcon /> : <LockClosedIcon />}
    </span>
  );
});
// A locked Field's lock (a guarded Field's, still editable, is a shield). Its
// label says why, in the tooltip and as the icon's accessible name. Focusable,
// so the tooltip also opens from the keyboard.
function InputLock({ lock }: { lock: FieldLock }) {
  return (
    <Tooltip content={lock.label} open={lock.open}>
      <LockIcon label={lock.label} shield={!lock.readOnly} />
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
