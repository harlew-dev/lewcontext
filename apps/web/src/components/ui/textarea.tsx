import type { ComponentPropsWithoutRef } from "react";
import { cx } from "@/lib/cx";
import { useField } from "./fieldset";
import { controlClasses, fieldClasses } from "./input";
type TextareaProps = Omit<
  ComponentPropsWithoutRef<"textarea">,
  "className" | "size"
> & {
  invalid?: boolean;
  resizable?: boolean;
  className?: string;
};
// A styled <textarea> (Base UI has no Textarea part) wired through our Field.
export function Textarea({
  invalid = false,
  resizable = true,
  className,
  ...props
}: TextareaProps) {
  const field = useField();
  const disabled = props.disabled ?? field?.disabled;
  return (
    <span data-slot="control" className={cx(controlClasses, className)}>
      <textarea
        id={field?.id}
        aria-describedby={field?.describedBy}
        {...props}
        disabled={disabled}
        aria-invalid={invalid || undefined}
        data-invalid={invalid ? "" : undefined}
        data-disabled={disabled ? "" : undefined}
        className={cx(
          fieldClasses,
          "h-full",
          resizable ? "resize-y" : "resize-none",
        )}
      />
    </span>
  );
}
