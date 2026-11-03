import { useEffect, useRef } from "react";
import { FunnelIcon } from "@heroicons/react/16/solid";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TextButton } from "@/components/ui/text";
import { Tooltip } from "@/components/ui/tooltip";
// The pieces of a filtered list's header, shared by the admin Events page and
// a workspace's Logs (docs/ui/forms-and-actions.md, List filters): a
// Filters button with the count of what's applied, and one removable badge
// per applied filter. The filters themselves live in a Dialog the page owns.

// Filters, with a zinc count badge once something is applied. `id` marks
// the button so focus can return to it.
export function FiltersButton({
  id,
  count,
  onClick,
}: {
  id: string;
  count: number;
  onClick: () => void;
}) {
  return (
    <span id={id} className="contents">
      <Button outline onClick={onClick}>
        <FunnelIcon />
        Filters
        {count > 0 && (
          <Badge>
            {count}
            <span className="sr-only"> applied</span>
          </Badge>
        )}
      </Button>
    </span>
  );
}
// One applied filter: its text, and a tooltip with the full text when the
// badge shortens it.
export type FilterBadge = { id: string; text: string; tooltip?: string };
// Removing a badge changes the page's URL; once it has, focus goes to the
// next badge, or the Filters button. Call `focusAfter(id)` with what
// `onRemove` was given, and pass the URL's search so the effect runs after
// the change.
export function useFocusAfterRemove(search: unknown, buttonId: string) {
  const next = useRef<string | null>(null);
  useEffect(() => {
    const target = next.current;
    if (!target) return;
    next.current = null;
    const element =
      target === "filters"
        ? document.getElementById(buttonId)?.querySelector("button")
        : document.querySelector<HTMLElement>(
            `[data-badge="${target}"] button[aria-label^="Remove filter"]`,
          );
    element?.focus();
  }, [search, buttonId]);
  return (id: string) => {
    next.current = id;
  };
}
// The applied filters, in the dialog's order, each removable; Clear all from
// two. The badge's text opens the dialog.
export function FilterBadges({
  badges,
  onRemove,
  onClearAll,
  onOpen,
}: {
  badges: FilterBadge[];
  // Takes the badge away, then the ID to focus once it has: the next
  // badge's, or "filters" for the Filters button.
  onRemove: (id: string, focus: string) => void;
  onClearAll: () => void;
  onOpen: () => void;
}) {
  if (badges.length === 0) return null;
  const nextOf = (i: number) =>
    badges[i + 1]?.id ?? badges[i - 1]?.id ?? "filters";
  return (
    <ul
      aria-label="Applied filters"
      className="mt-4 flex flex-wrap items-center gap-2"
    >
      {badges.map((badge, i) => (
        <li key={badge.id} data-badge={badge.id}>
          <Badge
            onRemove={() => onRemove(badge.id, nextOf(i))}
            removeLabel={`Remove filter: ${badge.text}`}
          >
            <BadgeText badge={badge} onClick={onOpen} />
          </Badge>
        </li>
      ))}
      {badges.length >= 2 && (
        <li>
          <TextButton className="text-sm/6" onClick={onClearAll}>
            Clear all
          </TextButton>
        </li>
      )}
    </ul>
  );
}
function BadgeText({
  badge,
  onClick,
}: {
  badge: FilterBadge;
  onClick: () => void;
}) {
  const button = (
    <button
      type="button"
      onClick={onClick}
      className="max-w-72 truncate text-left focus-visible:outline-2 focus-visible:outline-blue-500"
    >
      {badge.text}
    </button>
  );
  return badge.tooltip ? (
    <Tooltip content={badge.tooltip}>{button}</Tooltip>
  ) : (
    button
  );
}
