import type { ReactNode } from "react";
import { LevelBadge, type Level } from "./badge";
import { Divider } from "./divider";
import { Link } from "./link";
// A headline number under a rule: its title, the value ("–" until it has
// loaded), and a line of context below. For monitoring, it can also carry a
// level (a LevelBadge beside the title: the word, never colour alone), link
// its title to the page behind it, and show a chart (a Sparkline) under the
// value.
export function Stat({
  title,
  value,
  level,
  href,
  chart,
  children,
}: {
  title: string;
  value: ReactNode | undefined;
  level?: Level;
  href?: string;
  chart?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div>
      <Divider />
      <div className="mt-6 flex items-center justify-between gap-2">
        <div className="text-lg/6 font-medium sm:text-sm/6">
          {href ? (
            <Link
              href={href}
              className="data-focus:outline-2 data-focus:outline-blue-500 data-hover:underline"
            >
              {title}
            </Link>
          ) : (
            title
          )}
        </div>
        {level && <LevelBadge level={level} />}
      </div>
      <div className="mt-3 text-3xl/8 font-semibold sm:text-2xl/8">
        {value ?? "–"}
      </div>
      {chart && <div className="mt-3">{chart}</div>}
      {children && (
        <div className="mt-3 text-sm/6 sm:text-xs/6">{children}</div>
      )}
    </div>
  );
}
