import { useId, type ReactNode } from "react";
import { Checkbox as BaseCheckbox } from "@base-ui/react/checkbox";
import { ErrorMessage } from "./fieldset";
// A checkbox on Base UI's Checkbox (its hidden input keeps the form's
// `required` check and the value; the root is the focusable, announced
// checkbox; we draw the box). The whole row is its <label>: clicking the text
// ticks it, and a link in the text opens without ticking it. A description
// goes under the label (the label then in medium weight) and is read with the
// box. Inside a disabled Fieldset the checkbox is disabled with it.
const rowClasses =
  "group flex cursor-default items-start gap-3 data-disabled:opacity-50";
const boxClasses = [
  "relative mt-0.75 flex size-4.5 shrink-0 items-center justify-center rounded-[0.3125rem] border sm:mt-1 sm:size-4",
  "border-zinc-950/15 bg-white shadow-sm group-hover:border-zinc-950/30",
  "dark:border-white/15 dark:bg-white/5 dark:group-hover:border-white/30",
  "data-checked:border-zinc-900 data-checked:bg-zinc-900 dark:data-checked:border-white/10 dark:data-checked:bg-zinc-600",
  "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500",
  "data-invalid:border-red-500 dark:data-invalid:border-red-500",
  "forced-colors:data-checked:bg-[Highlight]",
].join(" ");
const labelClasses =
  "text-base/6 text-zinc-950 select-none sm:text-sm/6 dark:text-white";
// The check mark, drawn by the indicator once ticked.
function Checkmark() {
  return (
    <svg
      viewBox="0 0 14 14"
      fill="none"
      aria-hidden="true"
      className="size-3.5 stroke-white sm:size-3"
    >
      <path
        d="M3 8L6 11L11 3.5"
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
export type CheckboxProps = {
  checked: boolean;
  onChange: (checked: boolean) => void;
  // The label, which may hold links.
  children: ReactNode;
  // A line under the label, read with the box (aria-describedby).
  description?: ReactNode;
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
  description,
  name,
  required = false,
  disabled = false,
  invalid = false,
  "aria-describedby": describedBy,
}: CheckboxProps) {
  const descriptionId = useId();
  const describedByIds =
    [description ? descriptionId : undefined, describedBy]
      .filter(Boolean)
      .join(" ") || undefined;
  return (
    <label className={rowClasses} data-disabled={disabled ? "" : undefined}>
      <BaseCheckbox.Root
        checked={checked}
        onCheckedChange={onChange}
        name={name}
        required={required}
        disabled={disabled}
        aria-describedby={describedByIds}
        aria-invalid={invalid || undefined}
        data-invalid={invalid ? "" : undefined}
        className={boxClasses}
      >
        <BaseCheckbox.Indicator className="flex">
          <Checkmark />
        </BaseCheckbox.Indicator>
      </BaseCheckbox.Root>
      <span className={labelClasses}>
        {description ? (
          <>
            <span className="font-medium">{children}</span>
            <span
              id={descriptionId}
              className="block text-zinc-500 dark:text-zinc-400"
            >
              {description}
            </span>
          </>
        ) : (
          children
        )}
      </span>
    </label>
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
