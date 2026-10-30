import type { ReactNode } from "react";
import { FormControl } from "baseui/form-control";
import {
  Input as BaseInput,
  type InputProps as BaseInputProps,
} from "baseui/input";
import {
  Textarea as BaseTextarea,
  type TextareaProps as BaseTextareaProps,
} from "baseui/textarea";
type LabelProps = { label: string; caption?: string; id?: string };
// Labelled BaseUI form controls. The id defaults to the kebab-cased label.
function Labelled({
  label,
  caption,
  htmlFor,
  children,
}: Omit<LabelProps, "id"> & { htmlFor: string; children: ReactNode }) {
  return (
    <FormControl
      label={label}
      caption={caption}
      htmlFor={htmlFor}
      overrides={{
        Label: {
          style: { fontSize: "13px", fontWeight: 500, marginBottom: "7px" },
        },
      }}
    >
      {children}
    </FormControl>
  );
}
function controlId({ id, label }: LabelProps) {
  return id || label.toLowerCase().replaceAll(" ", "-");
}
export function Input({
  label,
  caption,
  id,
  minLength,
  ...props
}: BaseInputProps & LabelProps & { minLength?: number }) {
  const inputId = controlId({ id, label });
  return (
    <Labelled label={label} caption={caption} htmlFor={inputId}>
      <BaseInput
        id={inputId}
        size="compact"
        overrides={{ Input: { props: { minLength } } }}
        {...props}
      />
    </Labelled>
  );
}
export function Textarea({
  label,
  caption,
  id,
  ...props
}: BaseTextareaProps & LabelProps) {
  const textareaId = controlId({ id, label });
  return (
    <Labelled label={label} caption={caption} htmlFor={textareaId}>
      <BaseTextarea id={textareaId} {...props} />
    </Labelled>
  );
}
