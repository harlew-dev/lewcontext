import { useId, type ReactNode } from "react";
import { Checkbox as BaseCheckbox } from "baseui/checkbox";
import { slot } from "@/lib/baseui";
// A checkbox and its label on top of BaseUI's Checkbox, which keeps the
// native input (hidden, but focusable and announced) and tracks hover and
// keyboard focus. The label is the children; a description goes under it
// and is read with the box (aria-describedby). Inside a disabled Fieldset
// the native input is disabled with it.
const checkedAttribute = (state: Record<string, unknown>) => ({
  "data-checked": state.$checked ? "" : undefined,
});
const CheckboxRoot = slot(
  "label",
  "group flex cursor-default items-start gap-3 data-disabled:opacity-50",
  { props: checkedAttribute },
);
// The box: white with a zinc border, dark when checked, a focus ring for
// the keyboard.
const CheckboxMark = slot(
  "span",
  "relative mt-0.5 flex size-4.5 shrink-0 items-center justify-center rounded-[0.3125rem] border border-zinc-950/15 bg-white shadow-sm data-checked:border-transparent data-checked:bg-zinc-900 data-focus:outline-2 data-focus:outline-offset-2 data-focus:outline-blue-500 data-hover:border-zinc-950/30 sm:size-4 dark:border-white/15 dark:bg-white/5 dark:data-checked:bg-zinc-600 dark:data-hover:border-white/30",
  { props: checkedAttribute },
);
const CheckboxInput = slot("input", "sr-only");
const CheckboxLabel = slot(
  "span",
  "text-base/6 text-zinc-950 select-none sm:text-sm/6 dark:text-white",
);
// The tick, shown only while checked.
const tick = (
  <svg
    viewBox="0 0 14 14"
    fill="none"
    aria-hidden="true"
    className="size-3.5 stroke-white opacity-0 group-data-checked:opacity-100 sm:size-3"
  >
    <path
      d="M3 8L6 11L11 3.5"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);
export function Checkbox({
  checked,
  onChange,
  name,
  description,
  disabled,
  children,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  name?: string;
  // A line under the label, read with the box.
  description?: ReactNode;
  disabled?: boolean;
  // The label.
  children: ReactNode;
}) {
  const descriptionId = useId();
  return (
    <BaseCheckbox
      checked={checked}
      onChange={(event) => onChange(event.currentTarget.checked)}
      name={name}
      disabled={disabled}
      aria-describedby={description ? descriptionId : undefined}
      overrides={{
        Root: { component: CheckboxRoot },
        Checkmark: { component: CheckboxMark, props: { children: tick } },
        Input: { component: CheckboxInput },
        Label: { component: CheckboxLabel },
      }}
    >
      <span className="font-medium">{children}</span>
      {description && (
        <span
          id={descriptionId}
          className="block text-zinc-500 dark:text-zinc-400"
        >
          {description}
        </span>
      )}
    </BaseCheckbox>
  );
}
