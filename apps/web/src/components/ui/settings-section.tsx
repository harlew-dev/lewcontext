import { Collapsible } from "@base-ui/react/collapsible";
import { ChevronDownIcon } from "@heroicons/react/16/solid";
import type { ReactNode } from "react";
import { cx } from "@/lib/cx";
import { Button } from "./button";
import { Subheading } from "./heading";
import { Text } from "./text";
// A settings group: title (and a description, only when the title and the
// content don't already say enough) on the left, content on the right
// (stacked on small screens). `stacked` puts the content under the title at
// full width instead, for rows of actions. The "danger" variant is the danger
// zone: always stacked and always collapsed, with no description. Its header
// is only the red title and a chevron, one button that opens the content
// (a red-tinted panel whose full-width rows are DangerAction) below it.
// Collapsing is a convenience, not a safeguard: each action confirms itself.
export function SettingsSection({
  title,
  description,
  variant = "default",
  stacked = false,
  children,
}: {
  title: string;
  description?: string;
  variant?: "default" | "danger";
  stacked?: boolean;
  children: ReactNode;
}) {
  if (variant === "danger")
    return <DangerZone title={title}>{children}</DangerZone>;
  return (
    <section
      className={cx("grid gap-x-8 gap-y-6", !stacked && "sm:grid-cols-2")}
    >
      <div className="space-y-1">
        <Subheading>{title}</Subheading>
        {description && <Text>{description}</Text>}
      </div>
      {children}
    </section>
  );
}
// Base UI's trigger props type includes the HTML `color` attribute, which
// Button's own `color` prop can't take.
function withoutColor<T extends { color?: string }>(props: T) {
  const rest = { ...props };
  delete rest.color;
  return rest as Omit<T, "color">;
}
function DangerZone({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <Collapsible.Root render={<section />}>
      <div className="flex items-center justify-between gap-4">
        <Subheading>
          <span className="text-red-600 dark:text-red-400">{title}</span>
        </Subheading>
        <Collapsible.Trigger
          render={(props) => (
            <Button plain {...withoutColor(props)} className="group shrink-0">
              <span className="sr-only">
                {props["aria-expanded"] ? "Hide" : "Show"} {title.toLowerCase()}
              </span>
              <ChevronDownIcon
                data-slot="icon"
                className="transition-transform duration-200 group-data-panel-open:rotate-180 motion-reduce:transition-none"
              />
            </Button>
          )}
        />
      </div>
      <Collapsible.Panel
        keepMounted
        className="group relative h-(--collapsible-panel-height) overflow-hidden transition-[height] duration-200 ease-in-out data-ending-style:h-0 data-starting-style:h-0 motion-reduce:transition-none"
      >
        {/* Blurs and fades the content with the height, as Animate UI's
            collapsible does. */}
        <div className="pt-6 transition-[opacity,filter] duration-200 ease-in-out group-data-ending-style:opacity-0 group-data-ending-style:blur-xs group-data-starting-style:opacity-0 group-data-starting-style:blur-xs motion-reduce:transition-none">
          <div className="divide-y divide-red-600/10 rounded-xl border border-red-600/20 bg-red-500/5 px-4 dark:divide-red-500/15 dark:border-red-500/25 dark:bg-red-500/10">
            {children}
          </div>
        </div>
        {/* Fades the content in from the card's background while the panel
            opens and out into it while it closes, so the edge isn't a cut. */}
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-x-0 bottom-0 h-10 bg-linear-to-b from-transparent to-white opacity-0 transition-opacity duration-200 ease-in-out group-data-ending-style:opacity-100 group-data-starting-style:opacity-100 motion-reduce:transition-none dark:to-zinc-900"
        />
      </Collapsible.Panel>
    </Collapsible.Root>
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
