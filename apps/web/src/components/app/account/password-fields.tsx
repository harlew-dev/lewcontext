import {
  Description,
  ErrorMessage,
  Field,
  FieldGroup,
  Label,
} from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";
// Password inputs shared by the change-password dialog (account settings) and
// the reset link page (/auth/forgot-password/<token>).
export const minimumPasswordLength = 12;
// Checks the browser can make before sending a new password: errors keyed
// "newPassword" or "confirmation", for NewPasswordFields.
export function newPasswordProblems(
  password: string,
  confirmation: string,
): Record<string, string> {
  if ([...password].length < minimumPasswordLength)
    return { newPassword: `Use at least ${minimumPasswordLength} characters.` };
  if (password !== confirmation)
    return { confirmation: "The passwords don’t match." };
  return {};
}
export function PasswordField({
  label,
  description,
  autoComplete,
  value,
  onChange,
  error,
  disabled,
  autoFocus = true,
}: {
  label: string;
  description?: string;
  autoComplete: string;
  value: string;
  onChange: (value: string) => void;
  error?: string;
  disabled: boolean;
  autoFocus?: boolean;
}) {
  return (
    <Field disabled={disabled}>
      <Label>{label}</Label>
      {description && <Description>{description}</Description>}
      <Input
        type="password"
        autoComplete={autoComplete}
        autoFocus={autoFocus}
        required
        maxLength={128}
        invalid={Boolean(error)}
        value={value}
        onChange={(e) => onChange(e.currentTarget.value)}
      />
      {error && <ErrorMessage role="alert">{error}</ErrorMessage>}
    </Field>
  );
}
// A new password and its confirmation. `errors` holds messages keyed
// "newPassword" and "confirmation" (newPasswordProblems, or the API's field).
export function NewPasswordFields({
  password,
  confirmation,
  onPasswordChange,
  onConfirmationChange,
  errors,
  disabled,
}: {
  password: string;
  confirmation: string;
  onPasswordChange: (value: string) => void;
  onConfirmationChange: (value: string) => void;
  errors: Record<string, string>;
  disabled: boolean;
}) {
  return (
    <FieldGroup>
      <PasswordField
        label="New password"
        description={`At least ${minimumPasswordLength} characters.`}
        autoComplete="new-password"
        value={password}
        onChange={onPasswordChange}
        error={errors.newPassword}
        disabled={disabled}
      />
      <PasswordField
        label="Confirm new password"
        autoComplete="new-password"
        value={confirmation}
        onChange={onConfirmationChange}
        error={errors.confirmation}
        disabled={disabled}
        autoFocus={false}
      />
    </FieldGroup>
  );
}
