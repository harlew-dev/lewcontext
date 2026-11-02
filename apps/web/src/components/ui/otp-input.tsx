import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type SyntheticEvent,
} from "react";
import { Input as BaseInput } from "baseui/input";
import { slot } from "@/lib/baseui";
import { cx } from "@/lib/cx";
import { useField } from "./fieldset";
// A one-time code is 6 digits (an authenticator app's code).
export const otpLength = 6;
const boxClasses = [
  "relative flex size-11 items-center justify-center rounded-lg border font-mono text-lg/6 sm:size-10 sm:text-base/6",
  "bg-white text-zinc-950 shadow-sm dark:bg-white/5 dark:text-white dark:shadow-none",
  "border-zinc-950/10 group-hover:border-zinc-950/20 dark:border-white/10 dark:group-hover:border-white/20",
  // The fourth box starts the second group of three, as apps show it.
  "nth-4:ml-2",
  "data-active:outline-2 data-active:-outline-offset-1 data-active:outline-blue-500",
  "group-data-invalid:border-red-500 dark:group-data-invalid:border-red-600",
].join(" ");
// The real input lies over the boxes, invisible: it keeps typing, pasting,
// autofill, the label and screen readers working as in any text field.
const OtpRoot = slot("span", "absolute inset-0 block");
const OtpContainer = slot("span", "contents");
const OtpElement = slot(
  "input",
  "block size-full cursor-text bg-transparent text-transparent caret-transparent outline-hidden selection:bg-transparent",
);
export type OtpInputProps = {
  value: string;
  onChange: (value: string) => void;
  invalid?: boolean;
  // Submits the form once the last digit is in (the form's only field).
  autoSubmit?: boolean;
  autoFocus?: boolean;
  name?: string;
  className?: string;
};
// A 6-digit code drawn as six boxes, on one input (BaseUI's Input) rather
// than six, so a pasted "123 456", a password manager's fill and a screen
// reader all see one field. Only digits are kept. The caret stays after the
// last digit (Backspace deletes from the end) unless everything is selected.
// Inside a <Field> it takes the field's id, description and disabled state.
export const OtpInput = forwardRef<HTMLInputElement, OtpInputProps>(
  function OtpInput(
    {
      value,
      onChange,
      invalid = false,
      autoSubmit = false,
      autoFocus,
      name,
      className,
    },
    ref,
  ) {
    const field = useField();
    const input = useRef<HTMLInputElement>(null);
    useImperativeHandle(ref, () => input.current as HTMLInputElement);
    const [focused, setFocused] = useState(false);
    const digits = value.replace(/\D/g, "").slice(0, otpLength);
    useAutoSubmit(autoSubmit, digits, input);
    function keepCaretAtEnd(event: SyntheticEvent<HTMLInputElement>) {
      const el = event.currentTarget;
      const end = el.value.length;
      const all = el.selectionStart === 0 && el.selectionEnd === end;
      if (!all && (el.selectionStart !== end || el.selectionEnd !== end))
        el.setSelectionRange(end, end);
    }
    const active = Math.min(digits.length, otpLength - 1);
    return (
      <span
        data-slot="control"
        data-invalid={invalid ? "" : undefined}
        className={cx(
          className,
          "group relative block w-fit has-disabled:opacity-50",
        )}
      >
        <span aria-hidden="true" className="flex gap-2">
          {Array.from({ length: otpLength }, (_, i) => (
            <span
              key={i}
              data-active={focused && i === active ? "" : undefined}
              className={boxClasses}
            >
              {digits[i] ??
                (focused && i === digits.length && (
                  <span className="h-5 w-px animate-pulse bg-zinc-950 motion-reduce:animate-none dark:bg-white" />
                ))}
            </span>
          ))}
        </span>
        <BaseInput
          id={field?.id}
          aria-describedby={field?.describedBy}
          disabled={field?.disabled}
          name={name}
          autoFocus={autoFocus}
          autoComplete="one-time-code"
          inputMode="numeric"
          required
          value={digits}
          error={invalid}
          inputRef={input}
          onChange={(event) =>
            onChange(
              event.currentTarget.value.replace(/\D/g, "").slice(0, otpLength),
            )
          }
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          overrides={{
            Root: { component: OtpRoot },
            InputContainer: { component: OtpContainer },
            Input: {
              component: OtpElement,
              props: { onSelect: keepCaretAtEnd },
            },
          }}
        />
      </span>
    );
  },
);
// Submits the input's form when a complete code goes in, typed or pasted
// over another. In an effect, so the form's handler sees the new value; not
// on mount, so a field shown already full doesn't submit.
function useAutoSubmit(
  on: boolean,
  digits: string,
  input: React.RefObject<HTMLInputElement | null>,
) {
  const last = useRef(digits);
  useEffect(() => {
    const was = last.current;
    last.current = digits;
    if (on && digits.length === otpLength && digits !== was)
      input.current?.form?.requestSubmit();
  }, [on, digits, input]);
}
