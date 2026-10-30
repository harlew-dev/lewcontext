import type { ComponentPropsWithoutRef } from "react";
import { cx } from "@/lib/cx";
type AvatarProps = {
  src?: string | null;
  square?: boolean;
  initials?: string;
  alt?: string;
} & ComponentPropsWithoutRef<"span">;
export function Avatar({
  src = null,
  square = false,
  initials,
  alt = "",
  className,
  ...props
}: AvatarProps) {
  return (
    <span
      data-slot="avatar"
      {...props}
      className={cx(
        className,
        "inline-grid shrink-0 align-middle [--avatar-radius:20%] *:col-start-1 *:row-start-1",
        "outline -outline-offset-1 outline-black/10 dark:outline-white/10",
        square
          ? "rounded-(--avatar-radius) *:rounded-(--avatar-radius)"
          : "rounded-full *:rounded-full",
      )}
    >
      {initials && (
        <svg
          className="size-full fill-current p-[5%] text-[48px] font-medium uppercase select-none"
          viewBox="0 0 100 100"
          aria-hidden={alt ? undefined : "true"}
        >
          {alt && <title>{alt}</title>}
          <text
            x="50%"
            y="50%"
            alignmentBaseline="middle"
            dominantBaseline="middle"
            textAnchor="middle"
            dy=".125em"
          >
            {initials}
          </text>
        </svg>
      )}
      {src && <img className="size-full" src={src} alt={alt} />}
    </span>
  );
}
