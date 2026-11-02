import { useLocation } from "react-router";
import {
  ArrowLongLeftIcon,
  ArrowLongRightIcon,
} from "@heroicons/react/16/solid";
import { Button } from "@/components/ui/button";
// Older / Newer for the admin dashboard's cursor lists (Events, the Mail
// page's deliveries), which have no page numbers or totals. The URL holds
// only `before`, the cursor of the page shown, so a link to it stays
// shareable; the way back to newer pages travels in navigation state. Opened
// from a link, Newer goes to the newest page.

// The cursors of the pages before this one, newest first ("" is the newest
// page), from navigation state; anything else counts as none.
function trailOf(state: unknown): string[] {
  const cursors = (state as { cursors?: unknown } | null)?.cursors;
  if (!Array.isArray(cursors)) return [];
  return cursors.filter(
    (cursor): cursor is string => typeof cursor === "string",
  );
}
export function CursorNav({
  next,
  hrefFor,
  label,
}: {
  // The next, older page's cursor, or null on the last page.
  next: string | null;
  // The list's URL showing rows older than `before` (undefined: the newest).
  hrefFor: (before: string | undefined) => string;
  label: string;
}) {
  const location = useLocation();
  const before = new URLSearchParams(location.search).get("before") ?? "";
  const trail = trailOf(location.state);
  return (
    <nav aria-label={label} className="mt-6 flex justify-end gap-x-2">
      {before ? (
        <Button
          plain
          href={hrefFor(trail.at(-1) || undefined)}
          state={{ cursors: trail.slice(0, -1) }}
        >
          <ArrowLongLeftIcon />
          Newer
        </Button>
      ) : (
        <Button plain disabled>
          <ArrowLongLeftIcon />
          Newer
        </Button>
      )}
      {next ? (
        <Button
          plain
          href={hrefFor(next)}
          state={{ cursors: [...trail, before] }}
        >
          Older
          <ArrowLongRightIcon />
        </Button>
      ) : (
        <Button plain disabled>
          Older
          <ArrowLongRightIcon />
        </Button>
      )}
    </nav>
  );
}
