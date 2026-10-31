import type { ReactNode } from "react";
import { cx } from "@/lib/cx";
import { Subheading } from "./heading";
import { Text } from "./text";
// A settings group: title and description on the left, content on the right
// (stacked on small screens). `stacked` puts the content under the title at
// full width instead, for rows of actions. The "danger" variant is the danger
// zone, always stacked: a red title, and the content in a red-tinted panel
// whose full-width rows are DangerAction.
export function SettingsSection({
  title,
  description,
  variant = "default",
  stacked = false,
  children,
}: {
  title: string;
  description: string;
  variant?: "default" | "danger";
  stacked?: boolean;
  children: ReactNode;
}) {
  const danger = variant === "danger";
  return (
    <section
      className={cx(
        "grid gap-x-8 gap-y-6",
        !(stacked || danger) && "sm:grid-cols-2",
      )}
    >
      <div className="space-y-1">
        <Subheading>
          {danger ? (
            <span className="text-red-600 dark:text-red-400">{title}</span>
          ) : (
            title
          )}
        </Subheading>
        <Text>{description}</Text>
      </div>
      {danger ? (
        <div className="divide-y divide-red-600/10 rounded-xl border border-red-600/20 bg-red-500/5 px-4 dark:divide-red-500/15 dark:border-red-500/25 dark:bg-red-500/10">
          {children}
        </div>
      ) : (
        children
      )}
    </section>
  );
}
// One action in a danger zone, across the panel: what it does on the left,
// its button on the right (below it on phones).
export function DangerAction({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action: ReactNode;
}) {
  return (
    <div className="flex flex-col items-start gap-3 py-4 sm:flex-row sm:items-center sm:justify-between sm:gap-6">
      <div className="min-w-0">
        <div className="text-base/6 font-medium text-zinc-950 sm:text-sm/6 dark:text-white">
          {title}
        </div>
        <Text>{description}</Text>
      </div>
      <div className="shrink-0">{action}</div>
    </div>
  );
}
