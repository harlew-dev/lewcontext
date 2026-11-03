import { useId, type ReactNode } from "react";
import { Radio as BaseRadio } from "@base-ui/react/radio";
import { RadioGroup as BaseRadioGroup } from "@base-ui/react/radio-group";
import { cx } from "@/lib/cx";
// A choice of one among a few, on Base UI's RadioGroup and Radio (arrow keys
// move between them, the group is announced with its label). Each option is
// its own <label>, so clicking the text picks it, with a description under it
// read with the radio. Styled like Checkbox, round.
const rowClasses =
  "group flex cursor-default items-start gap-3 data-disabled:opacity-50";
const circleClasses = [
  "relative mt-0.75 flex size-4.5 shrink-0 items-center justify-center rounded-full border sm:mt-1 sm:size-4",
  "border-zinc-950/15 bg-white shadow-sm group-hover:border-zinc-950/30",
  "dark:border-white/15 dark:bg-white/5 dark:group-hover:border-white/30",
  "data-checked:border-zinc-900 data-checked:bg-zinc-900 dark:data-checked:border-white/10 dark:data-checked:bg-zinc-600",
  "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500",
].join(" ");
const labelClasses =
  "text-base/6 text-zinc-950 select-none sm:text-sm/6 dark:text-white";
export type RadioOption<Value extends string> = {
  value: Value;
  label: ReactNode;
  // A line under the label, read with the radio.
  description?: ReactNode;
};
export function RadioGroup<Value extends string>({
  value,
  onChange,
  options,
  label,
  disabled = false,
  className,
}: {
  value: Value;
  onChange: (value: Value) => void;
  options: RadioOption<Value>[];
  // The group's accessible name.
  label: string;
  disabled?: boolean;
  className?: string;
}) {
  const id = useId();
  return (
    <BaseRadioGroup
      value={value}
      onValueChange={(next) => onChange(next as Value)}
      disabled={disabled}
      aria-label={label}
      className={cx("space-y-3", className)}
    >
      {options.map((option) => {
        const descriptionId = `${id}-${option.value}`;
        return (
          <label
            key={option.value}
            className={rowClasses}
            data-disabled={disabled ? "" : undefined}
          >
            <BaseRadio.Root
              value={option.value}
              aria-describedby={option.description ? descriptionId : undefined}
              className={circleClasses}
            >
              <BaseRadio.Indicator className="size-1.5 rounded-full bg-white" />
            </BaseRadio.Root>
            <span className={labelClasses}>
              <span className={option.description ? "font-medium" : undefined}>
                {option.label}
              </span>
              {option.description && (
                <span
                  id={descriptionId}
                  className="block text-zinc-500 dark:text-zinc-400"
                >
                  {option.description}
                </span>
              )}
            </span>
          </label>
        );
      })}
    </BaseRadioGroup>
  );
}
