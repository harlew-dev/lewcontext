import { Fragment, useId, useState } from "react";
import { useSearchParams } from "react-router";
import {
  ChevronRightIcon,
  EllipsisHorizontalIcon,
} from "@heroicons/react/16/solid";
import { MemberLink } from "@/components/app/members/member-link";
import { Button } from "@/components/ui/button";
import { CursorNav } from "@/components/ui/cursor-nav";
import {
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import {
  FilterBadges,
  FiltersButton,
  useFocusAfterRemove,
  type FilterBadge,
} from "@/components/ui/filter-bar";
import { RangeDialog } from "@/components/ui/range-dialog";
import { SearchField } from "@/components/ui/search-field";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { TimeRange } from "@/components/ui/time-range";
import { TextLink } from "@/components/ui/text";
import type { ApiPath, EventIndexQuery } from "@/lib/api";
import { cx } from "@/lib/cx";
import {
  eventLabels,
  eventSentence,
  eventSummary,
  whoIs,
} from "@/lib/event-log";
import { formatDateTime } from "@/lib/format";
import { routes } from "@/lib/routes";
import { useResource } from "@/lib/use-resource";
import {
  eventTypes,
  eventWindows,
  type EventPage,
  type EventQuery,
  type WorkspaceEvent,
} from "@/types/events";
import { EventDetails, type EventLinks } from "./event-details";
import {
  EventFiltersDialog,
  type FilterChoices,
  type FilterPatch,
} from "./event-filters-dialog";
import {
  eventQuery,
  eventSearch,
  indexQuery,
  timeLabel,
} from "./event-filters";
// A list of a workspace's changes with its controls, shared by the Logs tab,
// a member's Logs and an API key's Logs: a search, the time range and
// Filters in the header, the applied filters as badges, then the changes
// newest first, each opening in place and, in its ⋯ menu, to its own page.
// The state is in the URL (other parameters, a page's ?tab=, are kept), so a
// link opens the same view. Reading it records nothing.
export function EventLog({
  label,
  pathFor,
  links,
  choices,
}: {
  // Names the list for screen readers ("Logs").
  label: string;
  // The API path for a query: a workspace's log, narrowed to a member or a
  // key by the page, or a personal key's own.
  pathFor: (query: EventIndexQuery) => ApiPath<EventPage>;
  // Where its members and changes can be opened, or null.
  links: EventLinks | null;
  // The people and projects to filter by, or null for none.
  choices: FilterChoices;
}) {
  const [search, setSearch] = useSearchParams();
  const query = eventQuery(search);
  const list = useResource(pathFor(indexQuery(query)));
  const [dialog, setDialog] = useState<"filters" | "range" | null>(null);
  const filtersButton = useId();
  const focusAfter = useFocusAfterRemove(search, filtersButton);
  const go = (patch: Partial<EventQuery>, replace = false) =>
    setSearch(eventSearch(search, { ...query, ...patch, before: "" }), {
      replace,
      preventScrollReset: true,
    });
  const refs = list.data?.refs;
  const badges = appliedBadges(query, refs);
  const apply = (patch: FilterPatch) => {
    setDialog(null);
    go(patch);
  };
  return (
    <>
      <div className="flex flex-wrap items-end gap-3">
        <div className="w-full sm:w-64">
          <SearchField
            label="Search changes"
            value={query.q}
            onSearch={(q) => go({ q }, true)}
          />
        </div>
        <TimeRange
          value={query.window}
          presets={eventWindows}
          custom={timeLabel(query)}
          onPreset={(window) => go({ window, from: "", to: "" })}
          onCustom={() => setDialog("range")}
        />
        <FiltersButton
          id={filtersButton}
          count={badges.length}
          onClick={() => setDialog("filters")}
        />
      </div>
      <FilterBadges
        badges={badges}
        onRemove={(id, focus) => {
          focusAfter(focus);
          go(without(id));
        }}
        onClearAll={() => {
          focusAfter("filters");
          go({ types: [], actor: "", project: "", q: "" });
        }}
        onOpen={() => setDialog("filters")}
      />
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      <div className="mt-6">
        {list.loading && !list.data ? (
          <ListSkeleton label="Loading logs" rows={5} />
        ) : list.data && list.data.data.length > 0 ? (
          <div
            aria-busy={list.refreshing}
            className={cx(list.refreshing && "opacity-60")}
          >
            <EventTable label={label} events={list.data.data} links={links} />
            <CursorNav
              label={`${label} pages`}
              next={list.data.next}
              hrefFor={(before) =>
                `?${eventSearch(search, { ...query, before: before ?? "" })}`
              }
            />
          </div>
        ) : list.data ? (
          <EmptyState
            title={
              badges.length > 0
                ? "No changes match these filters"
                : "No changes in this period"
            }
            description={
              badges.length > 0
                ? "Try a different search, type or person."
                : "Changes to the workspace, its members, invitations, projects and API keys appear here."
            }
            action={
              badges.length > 0 ? (
                <Button
                  outline
                  onClick={() =>
                    go({ types: [], actor: "", project: "", q: "" })
                  }
                >
                  Clear filters
                </Button>
              ) : undefined
            }
          />
        ) : null}
      </div>
      <EventFiltersDialog
        open={dialog === "filters"}
        onClose={() => setDialog(null)}
        value={{
          types: query.types,
          actor: query.actor,
          project: query.project,
        }}
        choices={namedChoices(choices, refs)}
        onApply={apply}
      />
      <RangeDialog
        open={dialog === "range"}
        onClose={() => setDialog(null)}
        from={query.from || undefined}
        to={query.to || undefined}
        description="Days and times in UTC. Changes are kept for 90 days."
        onApply={(from, to) => {
          setDialog(null);
          go({ from, to });
        }}
      />
    </>
  );
}
// The state with one applied filter taken away.
function without(id: string): Partial<EventQuery> {
  if (id === "types") return { types: [] };
  if (id === "actor") return { actor: "" };
  if (id === "project") return { project: "" };
  return { q: "" };
}
// One badge per applied filter, in the dialog's order. The time range is
// not one: it has its own control.
function appliedBadges(
  query: EventQuery,
  refs: EventPage["refs"] | undefined,
): FilterBadge[] {
  const badges: FilterBadge[] = [];
  if (query.types.length > 0) {
    const names = query.types.map(
      (t) => eventTypes.find((g) => g.id === t)?.label ?? eventLabels[t] ?? t,
    );
    badges.push({
      id: "types",
      text: `Type: ${names.slice(0, 2).join(", ")}${names.length > 2 ? ` +${names.length - 2}` : ""}`,
      tooltip: names.length > 2 ? names.join(", ") : undefined,
    });
  }
  if (query.actor)
    badges.push({
      id: "actor",
      text: `Person: ${refs?.actor?.id === query.actor && refs.actor.name ? refs.actor.name : "A person"}`,
    });
  if (query.project)
    badges.push({
      id: "project",
      text: `Project: ${refs?.project?.id === query.project && refs.project.name ? refs.project.name : query.project}`,
    });
  if (query.q) badges.push({ id: "q", text: `Search: ${query.q}` });
  return badges;
}
// The pickers' lists with the applied person named from the response, for a
// former member the members list no longer holds.
function namedChoices(
  choices: FilterChoices,
  refs: EventPage["refs"] | undefined,
): FilterChoices {
  const ref = refs?.actor;
  if (!choices.people || !ref?.name) return choices;
  return {
    ...choices,
    people: choices.people.some((p) => p.id === ref.id)
      ? choices.people
      : [...choices.people, ref],
  };
}
function EventTable({
  label,
  events,
  links,
}: {
  label: string;
  events: WorkspaceEvent[];
  links: EventLinks | null;
}) {
  return (
    <Table dense aria-label={label}>
      <TableHead>
        <TableRow>
          <TableHeader className="w-0">
            <span className="sr-only">Details</span>
          </TableHeader>
          <TableHeader>Time (UTC)</TableHeader>
          <TableHeader>Change</TableHeader>
          <TableHeader className="w-0">
            <span className="sr-only">Actions</span>
          </TableHeader>
        </TableRow>
      </TableHead>
      <TableBody>
        {events.map((event) => (
          <EventRow key={event.id} event={event} links={links} />
        ))}
      </TableBody>
    </Table>
  );
}
// A change in one line, opening in place to its details (the list already
// holds them: nothing more is loaded), and a ⋯ menu to its page.
function EventRow({
  event,
  links,
}: {
  event: WorkspaceEvent;
  links: EventLinks | null;
}) {
  const detailsId = useId();
  const [open, setOpen] = useState(false);
  const sentence = eventSentence(event);
  const summary = eventSummary(event);
  const who = whoIs(event.actor, event.apiKey);
  const page = links?.events
    ? routes.events.show(links.workspace, event.id)
    : null;
  return (
    <Fragment>
      <TableRow className={cx(open && "*:border-b-0")}>
        <TableCell className="w-0 pr-0">
          <Button
            plain
            aria-expanded={open}
            aria-controls={detailsId}
            aria-label={`Details: ${who} ${sentence}`}
            onClick={() => setOpen(!open)}
            className="-my-1.5"
          >
            <ChevronRightIcon
              className={cx(
                "transition-transform motion-reduce:transition-none",
                open && "rotate-90",
              )}
            />
          </Button>
        </TableCell>
        <TableCell className="align-top whitespace-nowrap tabular-nums">
          {page ? (
            <TextLink href={page}>{shortTime(event.createdAt)}</TextLink>
          ) : (
            shortTime(event.createdAt)
          )}
        </TableCell>
        <TableCell className="max-w-0 whitespace-normal">
          <div className="break-words">
            {event.actor ? (
              <MemberLink
                workspace={links?.members ? links.workspace : null}
                member={event.actor}
                className="font-medium"
              />
            ) : (
              <span className="font-medium">{who}</span>
            )}{" "}
            {sentence}
          </div>
          {summary && (
            <div className="truncate text-zinc-500 dark:text-zinc-400">
              {summary}
            </div>
          )}
        </TableCell>
        <TableCell className="w-0 text-right">
          {page && (
            <Dropdown>
              <DropdownButton
                plain
                aria-label={`Actions for ${who} ${sentence}`}
              >
                <EllipsisHorizontalIcon data-slot="icon" />
              </DropdownButton>
              <DropdownMenu>
                <DropdownItem href={page}>View event</DropdownItem>
              </DropdownMenu>
            </Dropdown>
          )}
        </TableCell>
      </TableRow>
      {open && (
        <TableRow id={detailsId}>
          <TableCell colSpan={4} className="whitespace-normal">
            <EventDetails event={event} links={links} />
          </TableCell>
        </TableRow>
      )}
    </Fragment>
  );
}
// "Oct 31, 2026, 2:05 PM" without its zone: the column says UTC.
function shortTime(value: string) {
  return formatDateTime(value).replace(/ UTC$/, "");
}
