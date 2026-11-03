import { Collapsible } from "@base-ui/react/collapsible";
import { ChevronDownIcon } from "@heroicons/react/16/solid";
import type { ReactNode } from "react";
import { cx } from "@/lib/cx";
// One collapsible item on Base UI's Collapsible: a full-width header button
// (the title, an optional line of `summary` beside it, and a chevron that
// turns) over a panel whose height animates, collapsed unless `defaultOpen`.
// The trigger is announced as expanded or collapsed, and a closed panel stays
// mounted but hidden. Lists of them are separated by a rule (`divide-y` on
// the wrapper); `bordered` draws a card of its own.
export function Disclosure({
  title,
  summary,
  defaultOpen = false,
  bordered = false,
  className,
  children,
}: {
  title: ReactNode;
  // Read beside the title, e.g. a badge or "3 signals".
  summary?: ReactNode;
  defaultOpen?: boolean;
  bordered?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <Collapsible.Root
      defaultOpen={defaultOpen}
      className={cx(
        bordered &&
          "rounded-xl border border-zinc-950/10 px-4 dark:border-white/10",
        className,
      )}
    >
      <Collapsible.Trigger className="group flex w-full cursor-default items-center justify-between gap-4 py-3 text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500">
        <span className="min-w-0 text-base/6 font-medium text-zinc-950 sm:text-sm/6 dark:text-white">
          {title}
        </span>
        <span className="flex shrink-0 items-center gap-3 text-sm/6 text-zinc-500 dark:text-zinc-400">
          {summary}
          <ChevronDownIcon
            aria-hidden="true"
            className="size-4 transition-transform duration-200 group-data-panel-open:rotate-180 motion-reduce:transition-none"
          />
        </span>
      </Collapsible.Trigger>
      <Collapsible.Panel
        keepMounted
        className="h-(--collapsible-panel-height) overflow-hidden transition-[height] duration-200 ease-in-out data-ending-style:h-0 data-starting-style:h-0 motion-reduce:transition-none"
      >
        <div className="pb-4">{children}</div>
      </Collapsible.Panel>
    </Collapsible.Root>
  );
}
