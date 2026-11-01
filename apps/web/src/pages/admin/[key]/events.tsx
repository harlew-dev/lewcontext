import { useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { CursorNav } from "@/components/app/admin/cursor-nav";
import { EventTable } from "@/components/app/admin/events";
import { Button } from "@/components/ui/button";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import { ErrorMessage, Field, Label } from "@/components/ui/fieldset";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { adminPaths } from "@/lib/api/admin";
import { useAdminKey } from "@/lib/admin";
import { cx } from "@/lib/cx";
import { parsePublicId } from "@/lib/ids";
import { adminRoutes } from "@/lib/admin-routes";
import { useAdminResource } from "@/lib/use-admin-resource";
import type { EventFilters } from "@/types/admin";
// The audit trail: every event, newest first, filtered by name, account,
// workspace, email, IP and time. Filters live in the URL, so a link from a
// user, a workspace or an IP opens the rows behind it. Every load is a
// recorded read (admin.event.listed, with its filters), so nothing reloads
// on its own: reloading the page does.
export default function AdminEventIndex() {
  const key = useAdminKey();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const filters = eventFilters(search);
  const list = useAdminResource(adminPaths.events.index(filters));
  const pathFor = (f: EventFilters) => adminRoutes.events.index(key, f);
  // Admin reads stay hidden unless asked for, so they don't count as a filter.
  const filtered = Object.keys(filters).some(
    (k) => k !== "before" && k !== "adminReads",
  );
  return (
    <>
      <Heading>Events</Heading>
      <FilterForm
        key={search.toString()}
        filters={filters}
        onApply={(f) => void navigate(pathFor(f), { preventScrollReset: true })}
        clearHref={filtered || filters.adminReads ? pathFor({}) : null}
      />
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      {list.data ? (
        list.data.data.length === 0 ? (
          <EmptyState
            title={filtered ? "No events match" : "No events yet"}
            description={
              filtered
                ? "Try fewer filters, or a wider time."
                : "Sign-ins, changes and admin reads appear here as they happen."
            }
          />
        ) : (
          <div
            aria-busy={list.refreshing}
            className={cx("mt-8", list.refreshing && "opacity-60")}
          >
            <EventTable events={list.data.data} />
            <CursorNav
              label="Event pages"
              next={list.data.next}
              hrefFor={(before) => pathFor({ ...filters, before })}
            />
          </div>
        )
      ) : (
        list.loading && <ListSkeleton label="Loading events" />
      )}
    </>
  );
}
// Values the API accepts, read from the URL; anything else is left out, so a
// hand-edited URL still shows a list rather than an error.
const filterPatterns: Record<
  Exclude<keyof EventFilters, "adminReads">,
  RegExp
> = {
  name: /^[a-z_]+(\.[a-z_]*){0,2}$/,
  account: /^usr_[0-9A-Za-z]{14}$/,
  workspace: /^wsp_[0-9A-Za-z]{14}$/,
  email: /^[^\s@]+@[^\s@]+$/,
  ip: /^[0-9A-Fa-f:.]+(\/\d{1,3})?$/,
  from: /^\d{4}-\d\d-\d\dT[\d:.]+(Z|[+-]\d\d:\d\d)$/,
  to: /^\d{4}-\d\d-\d\dT[\d:.]+(Z|[+-]\d\d:\d\d)$/,
  before: /^evt_[0-9A-Za-z]{14}$/,
};
function eventFilters(search: URLSearchParams): EventFilters {
  const filters: EventFilters = {};
  for (const [field, pattern] of Object.entries(filterPatterns)) {
    const value = search.get(field);
    if (value && value.length <= 254 && pattern.test(value))
      filters[field as keyof typeof filterPatterns] = value;
  }
  if (search.get("adminReads") === "include") filters.adminReads = "include";
  return filters;
}
// A date input's day (UTC) as the start of that day, and an inclusive "to"
// day as the start of the next; and back.
function dayStart(day: string, next = false): string | undefined {
  if (!day) return undefined;
  const date = new Date(`${day}T00:00:00Z`);
  if (Number.isNaN(date.getTime())) return undefined;
  if (next) date.setUTCDate(date.getUTCDate() + 1);
  return date.toISOString().replace(".000Z", "Z");
}
function dayOf(time: string | undefined, inclusiveEnd = false): string {
  if (!time) return "";
  const date = new Date(time);
  if (Number.isNaN(date.getTime())) return "";
  if (inclusiveEnd) date.setTime(date.getTime() - 1);
  return date.toISOString().slice(0, 10);
}
type FormValues = {
  name: string;
  account: string;
  workspace: string;
  email: string;
  ip: string;
  from: string;
  to: string;
  adminReads: "hidden" | "include";
};
const textFields = [
  { field: "account", label: "Account", placeholder: "usr_…" },
  { field: "workspace", label: "Workspace", placeholder: "wsp_…" },
  { field: "email", label: "Email", placeholder: "ada@example.com" },
  { field: "ip", label: "IP address or range", placeholder: "203.0.113.0/24" },
] as const;
// What's wrong with a typed value, or null.
function problem(
  field: (typeof textFields)[number]["field"],
  value: string,
): string | null {
  if (!value) return null;
  if (field === "account" && !parsePublicId("usr", value))
    return "An account ID starts with usr_.";
  if (field === "workspace" && !parsePublicId("wsp", value))
    return "A workspace ID starts with wsp_.";
  if (field === "email" && !filterPatterns.email.test(value))
    return "Enter an email address.";
  if (field === "ip" && !filterPatterns.ip.test(value))
    return "Enter an address, or a range such as 10.0.0.0/16.";
  return null;
}
// The filters, applied together with Apply filters (each load is a
// recorded read, so typing doesn't search as it goes).
function FilterForm({
  filters,
  onApply,
  clearHref,
}: {
  filters: EventFilters;
  onApply: (filters: EventFilters) => void;
  clearHref: string | null;
}) {
  const [values, setValues] = useState<FormValues>({
    name: filters.name ?? "",
    account: filters.account ?? "",
    workspace: filters.workspace ?? "",
    email: filters.email ?? "",
    ip: filters.ip ?? "",
    from: dayOf(filters.from),
    to: dayOf(filters.to, true),
    adminReads: filters.adminReads ?? "hidden",
  });
  const [checked, setChecked] = useState(false);
  const set = (field: keyof FormValues) => (value: string) =>
    setValues((v) => ({ ...v, [field]: value }));
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setChecked(true);
    const clean = {
      ...values,
      email: values.email.trim().toLowerCase(),
      ip: values.ip.trim(),
    };
    if (textFields.some(({ field }) => problem(field, clean[field]))) return;
    onApply({
      name: clean.name || undefined,
      account: clean.account.trim() || undefined,
      workspace: clean.workspace.trim() || undefined,
      email: clean.email || undefined,
      ip: clean.ip || undefined,
      from: dayStart(clean.from),
      to: dayStart(clean.to, true),
      adminReads: clean.adminReads === "include" ? "include" : undefined,
    });
  };
  return (
    <form onSubmit={submit} className="mt-6" aria-label="Filter events">
      <div className="grid gap-x-4 gap-y-4 sm:grid-cols-2 lg:grid-cols-4">
        <Field>
          <Label>Event</Label>
          <EventNameListbox value={values.name} onChange={set("name")} />
        </Field>
        {textFields.map(({ field, label, placeholder }) => {
          const error = checked ? problem(field, values[field].trim()) : null;
          return (
            <Field key={field}>
              <Label>{label}</Label>
              <Input
                name={field}
                value={values[field]}
                placeholder={placeholder}
                invalid={error !== null}
                onChange={(e) => set(field)(e.currentTarget.value)}
              />
              {error && <ErrorMessage>{error}</ErrorMessage>}
            </Field>
          );
        })}
        <Field>
          <Label>From (UTC)</Label>
          <Input
            type="date"
            name="from"
            value={values.from}
            onChange={(e) => set("from")(e.currentTarget.value)}
          />
        </Field>
        <Field>
          <Label>To (UTC)</Label>
          <Input
            type="date"
            name="to"
            value={values.to}
            onChange={(e) => set("to")(e.currentTarget.value)}
          />
        </Field>
        <Field>
          <Label>Admin reads</Label>
          <Listbox value={values.adminReads} onChange={set("adminReads")}>
            <ListboxOption value="hidden">
              <ListboxLabel>Hidden</ListboxLabel>
            </ListboxOption>
            <ListboxOption value="include">
              <ListboxLabel>Shown</ListboxLabel>
            </ListboxOption>
          </Listbox>
        </Field>
      </div>
      <div className="mt-4 flex justify-end gap-x-2">
        {clearHref && (
          <Button plain href={clearHref}>
            Clear filters
          </Button>
        )}
        <Button type="submit">Apply filters</Button>
      </div>
    </form>
  );
}
// Every event name from the catalogue, grouped by area: each area's prefix
// ("All auth events") is an option, followed by its names.
function EventNameListbox({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const catalogue = useAdminResource(adminPaths.catalogue());
  const names =
    catalogue.data?.events ?? (value && !value.endsWith(".") ? [value] : []);
  const areas = [...new Set(names.map((name) => name.split(".")[0]))];
  // A prefix from a link may be deeper than an area (workspace.member.).
  const extra =
    value.endsWith(".") && !areas.some((area) => `${area}.` === value)
      ? [value]
      : [];
  return (
    <Listbox
      value={value || "all"}
      onChange={(v) => onChange(v === "all" ? "" : v)}
    >
      <ListboxOption value="all">
        <ListboxLabel>All events</ListboxLabel>
      </ListboxOption>
      {extra.map((prefix) => (
        <ListboxOption key={prefix} value={prefix}>
          <ListboxLabel>All {prefix.slice(0, -1)} events</ListboxLabel>
        </ListboxOption>
      ))}
      {areas.flatMap((area) => [
        <ListboxOption key={area} value={`${area}.`}>
          <ListboxLabel>All {area.replace("_", " ")} events</ListboxLabel>
        </ListboxOption>,
        ...names
          .filter((name) => name.startsWith(`${area}.`))
          .map((name) => (
            <ListboxOption key={name} value={name}>
              <ListboxLabel className="pl-4">{name}</ListboxLabel>
            </ListboxOption>
          )),
      ])}
    </Listbox>
  );
}
