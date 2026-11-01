import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ComponentPropsWithoutRef,
} from "react";
import { cx } from "@/lib/cx";
// A locked field's value can't be changed. `label` says why: the lock's tooltip
// and the control's description. `open` while the tooltip answers an attempt to
// edit the value (`show`), for a few seconds after the last one.
export type FieldLock = {
  label: string;
  open: boolean;
  show: () => void;
  hide: () => void;
};
// Field wires its label, description and error message to the control inside it,
// the way Headless UI's Field does.
type FieldContextValue = {
  id: string;
  labelId: string;
  describedBy: string | undefined;
  disabled: boolean;
  lock: FieldLock | undefined;
  register: (part: "description" | "error", present: boolean) => void;
  descriptionId: string;
  errorId: string;
};
const FieldContext = createContext<FieldContextValue | null>(null);
export function useField() {
  return useContext(FieldContext);
}
export function Fieldset({
  className,
  ...props
}: ComponentPropsWithoutRef<"fieldset">) {
  return (
    <fieldset
      {...props}
      className={cx(
        className,
        "*:data-[slot=text]:mt-1 [&>*+[data-slot=control]]:mt-6",
      )}
    />
  );
}
export function Legend({
  className,
  ...props
}: ComponentPropsWithoutRef<"legend">) {
  return (
    <legend
      data-slot="legend"
      {...props}
      className={cx(
        className,
        "text-base/6 font-semibold text-zinc-950 data-disabled:opacity-50 sm:text-sm/6 dark:text-white",
      )}
    />
  );
}
export function FieldGroup({
  className,
  ...props
}: ComponentPropsWithoutRef<"div">) {
  return (
    <div
      data-slot="control"
      {...props}
      className={cx(className, "space-y-8")}
    />
  );
}
const lockShownFor = 3000;
function useLock(locked: boolean | string): FieldLock | undefined {
  const [open, setOpen] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const hide = useCallback(() => {
    window.clearTimeout(timer.current);
    setOpen(false);
  }, []);
  const show = useCallback(() => {
    window.clearTimeout(timer.current);
    setOpen(true);
    timer.current = window.setTimeout(() => setOpen(false), lockShownFor);
  }, []);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return useMemo(() => {
    if (locked === false) return undefined;
    const label = locked === true ? "Can’t be changed" : locked;
    return { label, open, show, hide };
  }, [locked, open, show, hide]);
}
// `locked` (true, or why: "Your email can’t be changed") makes the
// Input read-only, puts a lock at the end of it, and shows the lock's tooltip
// when someone tries to type in it.
export function Field({
  className,
  disabled = false,
  locked = false,
  children,
  ...props
}: {
  disabled?: boolean;
  locked?: boolean | string;
} & ComponentPropsWithoutRef<"div">) {
  const id = useId();
  const lock = useLock(locked);
  const [parts, setParts] = useState({ description: false, error: false });
  // Stable, so Description/ErrorMessage register once rather than on every change.
  const register = useCallback(
    (part: "description" | "error", present: boolean) =>
      setParts((current) =>
        current[part] === present ? current : { ...current, [part]: present },
      ),
    [],
  );
  const value = useMemo<FieldContextValue>(() => {
    const descriptionId = `${id}-description`;
    const errorId = `${id}-error`;
    const describedBy = [
      parts.description && descriptionId,
      parts.error && errorId,
      lock && `${id}-lock`,
    ]
      .filter(Boolean)
      .join(" ");
    return {
      id: `${id}-control`,
      labelId: `${id}-label`,
      descriptionId,
      errorId,
      describedBy: describedBy || undefined,
      disabled,
      lock,
      register,
    };
  }, [id, parts, disabled, lock, register]);
  return (
    <FieldContext.Provider value={value}>
      {/* A visually hidden label (sr-only) adds no space above the control. */}
      <div
        {...props}
        data-disabled={disabled ? "" : undefined}
        className={cx(
          className,
          "[&>[data-slot=label]:not(.sr-only)+[data-slot=control]]:mt-3 [&>[data-slot=label]:not(.sr-only)+[data-slot=description]]:mt-1 [&>[data-slot=description]+[data-slot=control]]:mt-3 [&>[data-slot=control]+[data-slot=description]]:mt-3 [&>[data-slot=control]+[data-slot=error]]:mt-3 *:data-[slot=label]:font-medium",
        )}
      >
        {children}
        {/* Why it's locked, read with the control. The lock icon itself
            can't carry the id: BaseUI's Tooltip sets the id of its child. */}
        {lock && (
          <span id={`${id}-lock`} hidden>
            {lock.label}
          </span>
        )}
      </div>
    </FieldContext.Provider>
  );
}
export function Label({
  className,
  ...props
}: ComponentPropsWithoutRef<"label">) {
  const field = useField();
  return (
    <label
      data-slot="label"
      id={field?.labelId}
      htmlFor={field?.id}
      data-disabled={field?.disabled ? "" : undefined}
      {...props}
      className={cx(
        className,
        "text-base/6 text-zinc-950 select-none data-disabled:opacity-50 sm:text-sm/6 dark:text-white",
      )}
    />
  );
}
function useRegister(part: "description" | "error") {
  const register = useField()?.register;
  useEffect(() => {
    register?.(part, true);
    return () => register?.(part, false);
  }, [register, part]);
}
export function Description({
  className,
  ...props
}: ComponentPropsWithoutRef<"p">) {
  const field = useField();
  useRegister("description");
  return (
    <p
      data-slot="description"
      id={field?.descriptionId}
      data-disabled={field?.disabled ? "" : undefined}
      {...props}
      className={cx(
        className,
        "text-base/6 text-zinc-500 data-disabled:opacity-50 sm:text-sm/6 dark:text-zinc-400",
      )}
    />
  );
}
export function ErrorMessage({
  className,
  ...props
}: ComponentPropsWithoutRef<"p">) {
  const field = useField();
  useRegister("error");
  return (
    <p
      data-slot="error"
      id={field?.errorId}
      data-disabled={field?.disabled ? "" : undefined}
      {...props}
      className={cx(
        className,
        "text-base/6 text-red-600 data-disabled:opacity-50 sm:text-sm/6 dark:text-red-500",
      )}
    />
  );
}
