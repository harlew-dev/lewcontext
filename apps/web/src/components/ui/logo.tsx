import { cx } from "@/lib/cx";
// Forma wordmark: three stacked, skewed bars and the name. Inherits text colour.
export function Logo({ className }: { className?: string }) {
  return (
    <span
      className={cx(
        className,
        "inline-flex items-center gap-2 text-zinc-950 dark:text-white",
      )}
    >
      <svg
        viewBox="0 0 24 24"
        className="size-6 fill-current"
        aria-hidden="true"
      >
        <g transform="skewY(-12) translate(0 5)">
          <rect x="2" y="2" width="20" height="4.5" rx="1" />
          <rect x="2" y="9" width="15" height="4.5" rx="1" />
          <rect x="2" y="16" width="8" height="4.5" rx="1" />
        </g>
      </svg>
      <span className="text-lg/6 font-semibold tracking-tight">forma</span>
    </span>
  );
}
