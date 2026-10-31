import { appName } from "@/lib/app";
import { cx } from "@/lib/cx";
import { Link } from "./link";
// Wordmark: a small graph of linked nodes and the app name. Inherits text colour.
// With `href` it's a link (e.g. to the homepage), named by the app name.
export function Logo({
  className,
  href,
}: {
  className?: string;
  href?: string;
}) {
  const classes = cx(
    className,
    "inline-flex items-center gap-2 text-zinc-950 dark:text-white",
  );
  const mark = (
    <>
      <svg
        viewBox="0 0 24 24"
        className="size-6 fill-current stroke-current"
        aria-hidden="true"
      >
        <path
          d="M4.5 9 11 4l2 8.5L4.5 9M13 12.5l7-3M13 12.5 7.5 19M13 12.5l6 7"
          fill="none"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <circle cx="13" cy="12.5" r="3" />
        <circle cx="4.5" cy="9" r="2" />
        <circle cx="11" cy="4" r="2.25" />
        <circle cx="20" cy="9.5" r="1.75" />
        <circle cx="7.5" cy="19" r="2.25" />
        <circle cx="19" cy="19.5" r="1.75" />
      </svg>
      <span className="text-lg/6 font-semibold tracking-tight">{appName}</span>
    </>
  );
  if (href === undefined) return <span className={classes}>{mark}</span>;
  // w-fit: in a grid or flex column, only the wordmark itself is clickable.
  return (
    <Link href={href} className={cx(classes, "w-fit rounded-md")}>
      {mark}
    </Link>
  );
}
