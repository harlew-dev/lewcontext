import {
  Textarea as BaseTextarea,
  type TextareaProps as BaseTextareaProps,
} from "baseui/textarea";
import { slot } from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { useField } from "./fieldset";
import { controlClasses, fieldClasses } from "./input";
const TextareaControl = slot("span", controlClasses, {
  props: () => ({ "data-slot": "control" }),
});
const TextareaContainer = slot("span", "contents");
const TextareaElement = slot("textarea", cx(fieldClasses, "h-full"));
type TextareaProps = Omit<
  BaseTextareaProps,
  "overrides" | "size" | "error" | "resize"
> & {
  invalid?: boolean;
  resizable?: boolean;
  className?: string;
};
// Textarea on top of BaseUI's Textarea.
export function Textarea({
  invalid = false,
  resizable = true,
  className,
  ...props
}: TextareaProps) {
  const field = useField();
  return (
    <BaseTextarea
      id={field?.id}
      aria-describedby={field?.describedBy}
      disabled={field?.disabled}
      {...props}
      error={invalid}
      overrides={{
        Root: { component: TextareaControl, props: { className } },
        InputContainer: { component: TextareaContainer },
        Input: {
          component: TextareaElement,
          props: { className: resizable ? "resize-y" : "resize-none" },
        },
      }}
    />
  );
}
