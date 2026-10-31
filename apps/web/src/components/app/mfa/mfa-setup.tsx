import { useRef, useState } from "react";
import {
  ArrowDownTrayIcon,
  CheckIcon,
  DocumentDuplicateIcon,
  EyeIcon,
  EyeSlashIcon,
} from "@heroicons/react/16/solid";
import { Button } from "@/components/ui/button";
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
import { Reveal } from "@/components/ui/reveal";
import { Text, TextButton } from "@/components/ui/text";
import { Tooltip } from "@/components/ui/tooltip";
import type { MfaSetup } from "@/lib/api";
import { appName } from "@/lib/app";
import { cx } from "@/lib/cx";
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
  const [status, setStatus] = useState("");
  const show = useRef<HTMLElement>(null);
  const copied = status === "Copied";
  const groups = secret.match(/.{1,4}/g) ?? [secret];
  async function copy() {
    try {
      await navigator.clipboard.writeText(secret);
      setStatus("Copied");
    } catch {
      setStatus("Couldn’t copy. Show the key and copy it by hand instead.");
    }
  }
  function hide() {
    setShown(false);
    setVisible(false);
    setStatus("");
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
            <Tooltip content={copied ? "Copied" : "Copy"}>
              <InputButton aria-label="Copy setup key" onClick={copy}>
                {copied ? <CheckIcon /> : <DocumentDuplicateIcon />}
              </InputButton>
            </Tooltip>
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
      {/* "Copied" shows in the tooltip; screen readers hear it from here. */}
      <p
        role="status"
        className={cx(
          "text-sm/6 text-zinc-500 dark:text-zinc-400",
          copied && "sr-only",
        )}
      >
        {status}
      </p>
      <Text className="text-center">
        <TextButton onClick={hide}>Hide setup key</TextButton>
      </Text>
    </div>
  );
}
// A code field: an authenticator code (or, where allowed, a recovery code).
export function CodeField({
  label,
  description,
  value,
  onChange,
  error,
  disabled,
  autoFocus = false,
  recovery = false,
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
}) {
  return (
    <Field disabled={disabled}>
      <Label>{label}</Label>
      {description && <Description>{description}</Description>}
      <Input
        name="code"
        inputMode={recovery ? "text" : "numeric"}
        autoComplete={recovery ? "off" : "one-time-code"}
        autoFocus={autoFocus}
        required
        maxLength={recovery ? 32 : 7}
        invalid={Boolean(error)}
        className="font-mono"
        value={value}
        onChange={(e) => onChange(e.currentTarget.value)}
      />
      {error && <ErrorMessage role="alert">{error}</ErrorMessage>}
    </Field>
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
          value={codes.join("\n")}
          label="Copy codes"
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
// Copies value, saying whether it worked (where the clipboard isn't
// available, the text can still be selected by hand).
function CopyButton({
  value,
  label,
  onCopied,
}: {
  value: string;
  label: string;
  onCopied?: () => void;
}) {
  const [status, setStatus] = useState("");
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setStatus("Copied.");
      onCopied?.();
    } catch {
      setStatus("Couldn’t copy. Reveal it and copy it by hand instead.");
    }
  }
  return (
    <div className="flex flex-col items-center gap-1">
      <Button outline onClick={copy}>
        <DocumentDuplicateIcon />
        {label}
      </Button>
      <p role="status" className="text-sm/6 text-zinc-500 dark:text-zinc-400">
        {status}
      </p>
    </div>
  );
}
