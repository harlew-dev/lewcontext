import type { ButtonHTMLAttributes } from "react";
import { cx } from "@/lib/cx";
type IconButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  "aria-label": string;
};
// Borderless square button for a single icon. Callers must supply an accessible name.
export function IconButton({ className, ...props }: IconButtonProps) {
  return (
    <button
      type="button"
      className={cx(
        "inline-flex items-center justify-center rounded-[5px] p-1.75 text-haze-400 hover:bg-mauve-100 hover:text-haze-900",
        className,
      )}
      {...props}
    />
  );
}
