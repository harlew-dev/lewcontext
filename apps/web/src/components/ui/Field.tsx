import { FormControl } from "baseui/form-control";
import { Input, type InputProps } from "baseui/input";
import { Textarea } from "baseui/textarea";
export function Field({
  label,
  caption,
  id,
  minLength,
  ...props
}: InputProps & { label: string; caption?: string; minLength?: number }) {
  const inputId = id || label.toLowerCase().replaceAll(" ", "-");
  return (
    <FormControl
      label={label}
      caption={caption}
      htmlFor={inputId}
      overrides={{
        Label: {
          style: { fontSize: "13px", fontWeight: 500, marginBottom: "7px" },
        },
      }}
    >
      <Input
        id={inputId}
        size="compact"
        overrides={{ Input: { props: { minLength } } }}
        {...props}
      />
    </FormControl>
  );
}
export function TextAreaField({
  label,
  value,
  onChange,
  disabled,
}: {
  label: string;
  value: string;
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <FormControl
      label={label}
      htmlFor="project-description"
      overrides={{ Label: { style: { fontSize: "13px", fontWeight: 500 } } }}
    >
      <Textarea
        disabled={disabled}
        id="project-description"
        value={value}
        onChange={(event) => onChange(event.currentTarget.value)}
        maxLength={500}
        placeholder="What are you working toward?"
      />
    </FormControl>
  );
}
