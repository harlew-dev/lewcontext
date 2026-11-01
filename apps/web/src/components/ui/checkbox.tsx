import { useId, type ChangeEvent, type ReactNode } from "react";
import { Checkbox as BaseCheckbox } from "baseui/checkbox";
import { slot } from "@/lib/baseui";
import { ErrorMessage } from "./fieldset";
// A checkbox on BaseUI's Checkbox (its hidden input keeps the keyboard,
// the form's `required` check and screen readers; we draw the box). The
// whole row is its <label>: clicking the text ticks it, and a link in the
// text opens without ticking it.
const CheckboxRoot = slot(
  "label",
  "group flex cursor-default items-start gap-3 data-disabled:opacity-50",
);
const CheckmarkSlot = slot(
  "span",
  [
    "relative mt-0.75 flex size-4.5 shrink-0 items-center justify-center rounded-[0.3125rem] border sm:mt-1 sm:size-4",
    "border-zinc-950/15 bg-white shadow-sm data-hover:border-zinc-950/30",
    "dark:border-white/15 dark:bg-white/5 dark:data-hover:border-white/30",
    "data-checked:border-zinc-900 data-checked:bg-zinc-900 dark:data-checked:border-white/10 dark:data-checked:bg-zinc-600",
    "data-focus:outline-2 data-focus:outline-offset-2 data-focus:outline-blue-500",
    "data-invalid:border-red-500 dark:data-invalid:border-red-500",
    "forced-colors:data-checked:bg-[Highlight]",
  ].join(" "),
);
// The box, with its check mark drawn when ticked.
function CheckboxCheckmark(props: Record<string, unknown>) {
  return (
    <CheckmarkSlot {...props}>
      <svg
        viewBox="0 0 14 14"
        fill="none"
        aria-hidden="true"
        className="size-3.5 stroke-white opacity-0 group-has-checked:opacity-100 sm:size-3"
      >
        <path
          d="M3 8L6 11L11 3.5"
          strokeWidth={2}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </CheckmarkSlot>
  );
}
const CheckboxInput = slot("input", "sr-only");
const CheckboxLabel = slot(
  "span",
  "text-base/6 text-zinc-950 select-none sm:text-sm/6 dark:text-white",
);
const overrides = {
  Root: { component: CheckboxRoot },
  Checkmark: { component: CheckboxCheckmark },
  Input: { component: CheckboxInput },
  Label: { component: CheckboxLabel },
};
export type CheckboxProps = {
  checked: boolean;
  onChange: (checked: boolean) => void;
  // The label, which may hold links.
  children: ReactNode;
  name?: string;
  required?: boolean;
  disabled?: boolean;
  invalid?: boolean;
  "aria-describedby"?: string;
};
export function Checkbox({
  checked,
  onChange,
  children,
  name,
  required = false,
  disabled = false,
  invalid = false,
  "aria-describedby": describedBy,
}: CheckboxProps) {
  return (
    <BaseCheckbox
      checked={checked}
      onChange={(e: ChangeEvent<HTMLInputElement>) =>
        onChange(e.currentTarget.checked)
      }
      name={name}
      required={required}
      disabled={disabled}
      error={invalid}
      aria-describedby={describedBy}
      overrides={overrides}
    >
      {children}
    </BaseCheckbox>
  );
}
// A checkbox with its error under the label, read with it (an API's 422 on
// the field). Pass the label as children.
export function CheckboxField({
  error,
  className,
  ...props
}: CheckboxProps & { error?: string; className?: string }) {
  const errorId = useId();
  return (
    <div className={className}>
      <Checkbox
        {...props}
        invalid={Boolean(error)}
        aria-describedby={error ? errorId : undefined}
      />
      {error && (
        <ErrorMessage id={errorId} role="alert" className="mt-2 pl-7.5 sm:pl-7">
          {error}
        </ErrorMessage>
      )}
    </div>
  );
}
