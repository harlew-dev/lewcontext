import { useRef, useState } from "react";
import {
  ArrowDownTrayIcon,
  EyeIcon,
  EyeSlashIcon,
} from "@heroicons/react/16/solid";
import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/ui/copy-button";
import {
  Description,
  ErrorMessage,
  Field,
  Label,
} from "@/components/ui/fieldset";
import {
  Input,
  InputActions,
  InputButton,
  InputGroup,
} from "@/components/ui/input";
import { OtpInput } from "@/components/ui/otp-input";
import { Reveal } from "@/components/ui/reveal";
import { Text, TextButton } from "@/components/ui/text";
import { Tooltip } from "@/components/ui/tooltip";
import type { MfaSetup } from "@/lib/api";
import { appName } from "@/lib/app";
import { QrCode } from "./qr-code";
// Setting up two-factor authentication, shared by account settings and (the
// admin access plan) an admin's first sign-in. The page around it supplies
// the form, its buttons and the API calls; these are the parts in between.
// Nothing secret is on screen until asked for: the QR code, the setup key
// and the recovery codes each wait behind a button, in case the screen is
// shared or recorded.

// Stands in for the QR code until it's revealed: a code of nothing secret.
const decoy = "otpauth://totp/hidden?secret=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";

// The scan step: the QR code (or its key, to type in), then the first code
// from the app, which turns MFA on.
export function MfaScanStep({
  setup,
  code,
  onCodeChange,
  error,
  disabled,
}: {
  setup: MfaSetup;
  code: string;
  onCodeChange: (code: string) => void;
  error?: string;
  disabled: boolean;
}) {
  return (
    <div className="space-y-6">
      <Text>
        Scan the QR code with an authenticator app, such as 1Password, Google
        Authenticator or Microsoft Authenticator.
      </Text>
      <Reveal
        label="Reveal QR code"
        hideLabel="Hide QR code"
        announcement="QR code shown."
        placeholder={
          <div className="flex justify-center">
            <QrCode value={decoy} label="" />
          </div>
        }
      >
        <div className="flex justify-center">
          <QrCode
            value={setup.uri}
            label="QR code to add this account to your authenticator app"
          />
        </div>
      </Reveal>
      <SetupKey secret={setup.secret} />
      <CodeField
        label="Code from your app"
        description="The 6-digit code your app shows for this account."
        value={code}
        onChange={onCodeChange}
        error={error}
        disabled={disabled}
        autoSubmit
      />
    </div>
  );
}
// The secret as text, for apps that can't scan, behind "Use setup key
// instead" ("Hide setup key" puts it away). The field starts masked: the key
// isn't on the screen or in the page until the eye shows it, and Copy works
// either way. Focus follows the section's toggle both ways.
function SetupKey({ secret }: { secret: string }) {
  const [shown, setShown] = useState(false);
  const [visible, setVisible] = useState(false);
  const show = useRef<HTMLElement>(null);
  const groups = secret.match(/.{1,4}/g) ?? [secret];
  function hide() {
    setShown(false);
    setVisible(false);
    // Back to the link that showed it, once it's rendered.
    requestAnimationFrame(() => show.current?.focus());
  }
  if (!shown)
    return (
      <Text className="text-center">
        <TextButton ref={show} onClick={() => setShown(true)}>
          Use setup key instead
        </TextButton>
      </Text>
    );
  return (
    <div className="space-y-2">
      <Field>
        <Label className="sr-only">Setup key</Label>
        <InputGroup>
          <Input
            readOnly
            autoFocus
            value={
              visible
                ? groups.join(" ")
                : groups.map((g) => "•".repeat(g.length)).join(" ")
            }
            className="font-mono"
            onFocus={(e) => visible && e.currentTarget.select()}
          />
          <InputActions>
            <CopyButton
              value={secret}
              label="Copy setup key"
              failed="Couldn’t copy. Show the key and copy it by hand instead."
            />
            <Tooltip content={visible ? "Mask" : "Show"}>
              <InputButton
                aria-label={visible ? "Mask the key" : "Show the key"}
                aria-pressed={visible}
                onClick={() => setVisible(!visible)}
              >
                {visible ? <EyeSlashIcon /> : <EyeIcon />}
              </InputButton>
            </Tooltip>
          </InputActions>
        </InputGroup>
      </Field>
      <Text className="text-center">
        <TextButton onClick={hide}>Hide setup key</TextButton>
      </Text>
    </div>
  );
}
// A code field: an authenticator code in six boxes (OtpInput), or with
// `recovery` a recovery code in a text field. `autoSubmit` submits the form
// at the sixth digit, where the code is the form's only field.
export function CodeField({
  label,
  description,
  value,
  onChange,
  error,
  disabled,
  autoFocus = false,
  recovery = false,
  autoSubmit = false,
}: {
  label: string;
  description?: string;
  value: string;
  onChange: (value: string) => void;
  error?: string;
  disabled: boolean;
  autoFocus?: boolean;
  // Recovery codes are letters and digits; authenticator codes only digits.
  recovery?: boolean;
  autoSubmit?: boolean;
}) {
  return (
    <Field disabled={disabled}>
      <Label>{label}</Label>
      {description && <Description>{description}</Description>}
      {recovery ? (
        <Input
          name="code"
          autoComplete="off"
          autoFocus={autoFocus}
          required
          maxLength={32}
          invalid={Boolean(error)}
          className="font-mono"
          value={value}
          onChange={(e) => onChange(e.currentTarget.value)}
        />
      ) : (
        <OtpInput
          name="code"
          autoFocus={autoFocus}
          autoSubmit={autoSubmit}
          invalid={Boolean(error)}
          value={value}
          onChange={onChange}
        />
      )}
      {error && <ErrorMessage role="alert">{error}</ErrorMessage>}
    </Field>
  );
}
// A code from the authenticator app (six boxes) or, after "Lost your
// device?", a recovery code, where either one works: account settings'
// disabling MFA and replacing the recovery codes, and the admin dashboard's
// step-up (AdminActionDialog). Switching clears what was typed.
export function AppOrRecoveryCode({
  value,
  onChange,
  error,
  disabled,
  autoFocus = false,
  description,
}: {
  value: string;
  onChange: (value: string) => void;
  error?: string;
  disabled: boolean;
  // Focus the code when it appears (a step asked for after the fact).
  autoFocus?: boolean;
  // Why the code is asked for, read with the field.
  description?: string;
}) {
  const [recovery, setRecovery] = useState(false);
  return (
    <div className="space-y-3">
      <CodeField
        key={recovery ? "recovery" : "app"}
        label={recovery ? "Recovery code" : "Authentication code"}
        value={value}
        onChange={onChange}
        error={error}
        disabled={disabled}
        recovery={recovery}
        autoFocus={autoFocus}
        description={description}
      />
      <Text>
        <TextButton
          disabled={disabled}
          onClick={() => {
            setRecovery(!recovery);
            onChange("");
          }}
        >
          {recovery
            ? "Use your authenticator app instead"
            : "Lost your device? Use a recovery code"}
        </TextButton>
      </Text>
    </div>
  );
}
// New recovery codes, shown once: hidden until revealed, with Copy and
// Download (which work hidden too). onSaved runs the first time the codes
// are revealed, copied or downloaded, so the page can let the visitor go on.
export function RecoveryCodeList({
  codes,
  account,
  onSaved,
}: {
  codes: string[];
  // The account's email, named in the downloaded file.
  account: string;
  onSaved: () => void;
}) {
  function download() {
    const text = [
      `${appName} recovery codes for ${account}`,
      "Each code signs you in once, if you lose your authenticator app.",
      "",
      ...codes,
      "",
    ].join("\n");
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = `${appName}-recovery-codes.txt`;
    link.click();
    URL.revokeObjectURL(url);
    onSaved();
  }
  return (
    <div className="space-y-6">
      <Text>
        If you lose your authenticator app, each of these codes signs you in
        once. Keep them somewhere safe, such as your password manager. They
        won’t be shown again.
      </Text>
      <Reveal
        label="Reveal recovery codes"
        hideLabel="Hide recovery codes"
        announcement="Recovery codes shown."
        onReveal={onSaved}
        placeholder={<CodeGrid codes={codes.map(() => "xxxxx-xxxxx")} />}
      >
        <CodeGrid codes={codes} />
      </Reveal>
      <div className="flex flex-wrap justify-center gap-3">
        <CopyButton
          variant="button"
          value={codes.join("\n")}
          label="Copy codes"
          failed="Couldn’t copy. Reveal the codes and copy them by hand instead."
          onCopied={onSaved}
        />
        <Button outline onClick={download}>
          <ArrowDownTrayIcon />
          Download
        </Button>
      </div>
    </div>
  );
}
function CodeGrid({ codes }: { codes: string[] }) {
  return (
    <ul
      aria-label="Recovery codes"
      className="grid grid-cols-2 gap-x-6 gap-y-2 rounded-lg bg-zinc-950/2.5 px-6 py-4 text-center font-mono text-sm/6 text-zinc-950 dark:bg-white/5 dark:text-white"
    >
      {codes.map((code, i) => (
        <li key={i}>{code}</li>
      ))}
    </ul>
  );
}
