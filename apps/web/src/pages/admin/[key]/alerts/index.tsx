import { useNavigate, useSearchParams } from "react-router";
import { useId, useMemo, useState, type FormEvent } from "react";
import { EllipsisHorizontalIcon, FunnelIcon } from "@heroicons/react/16/solid";
import { AdminActionDialog } from "@/components/app/admin/admin-action-dialog";
import {
  alertBadges,
  alertListQuery,
  apiAlertQuery,
  catalogBadges,
  catalogQuery,
  filterRules,
  opened as openedLabel,
  windowLabels,
  type CatalogQuery,
} from "@/components/app/admin/alert-filters";
import { RangeDialog } from "@/components/ui/range-dialog";
import { AppOrRecoveryCode } from "@/components/app/mfa/mfa-setup";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Combobox, type ComboboxNode } from "@/components/ui/combobox";
import { CopyButton, CopyFailure, useCopy } from "@/components/ui/copy-button";
import {
  Dialog,
  DialogActions,
  DialogBody,
  DialogTitle,
} from "@/components/ui/dialog";
import { Disclosure } from "@/components/ui/disclosure";
import {
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { Field, FieldGroup, Label } from "@/components/ui/fieldset";
import { Input, InputActions, InputGroup } from "@/components/ui/input";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { RadioGroup } from "@/components/ui/radio-group";
import { SearchField } from "@/components/ui/search-field";
import { toast } from "@/components/ui/toast";
import { adminCan, useAdminSession } from "@/lib/admin";
import { errorMessage } from "@/lib/api";
import { needsStepUp, useAdminApi } from "@/lib/api/admin";
import { useAction } from "@/lib/use-action";
import { useOpenCount } from "@/lib/use-open-count";
import {
  alertHealths,
  alertStates,
  alertWebhookEvents,
  alertWindows,
  type AlertCatalog,
  type AlertListQuery,
  type AlertRule,
  type AlertState,
  type AlertWebhook,
  type AlertWebhookEvent,
  type AlertWebhookFormat,
  type AlertWebhookInput,
} from "@/types/admin";
import {
  AlertBadges,
  alertValue,
  openFor,
} from "@/components/app/admin/alerts";
import { CursorNav } from "@/components/ui/cursor-nav";
import {
  ErrorNotice,
  EmptyState,
  ListSkeleton,
  Notice,
} from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { Navbar, NavbarItem, NavbarSection } from "@/components/ui/navbar";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Text, TextButton, TextLink } from "@/components/ui/text";
import { useAdminKey } from "@/lib/admin";
import { adminRoutes } from "@/lib/admin-routes";
import { adminPaths } from "@/lib/api/admin";
import { cx } from "@/lib/cx";
import { formatDateTime } from "@/lib/format";
import { useAdminResource } from "@/lib/use-admin-resource";

// The alerts the evaluator opened and what to do about them
// (docs/api/monitoring.md#alerts), in three tabs: Alerts (the list, with its
// search, time range and filters in the URL), Catalog (every rule, each
// collapsible, with its own search and filters) and Subscriptions (your email
// and the shared webhooks). Only the Alerts tab has the table. Opening a tab
// is one recorded read. Acknowledging and silencing are on an alert's own
// page.
export default function AdminAlertIndex() {
  const key = useAdminKey();
  const [search] = useSearchParams();
  const view = search.get("view");
  const catalog = view === "catalog";
  const subscriptions = view === "subscriptions";
  // The Subscribe dialog: closed, or open to add (undefined) or to change one
  // (a webhook, or "email" for your own).
  const [dialog, setDialog] = useState<
    { edit?: AlertWebhook | "email" } | undefined
  >();
  const openedDialog = useOpenCount(dialog !== undefined);
  // The Subscriptions tab's two reads live here, so a save can read them
  // again at once (and each is one recorded read, not one per remount).
  const rules = useAdminResource(
    subscriptions ? adminPaths.alertRules.index() : null,
  );
  const hooks = useAdminResource(
    subscriptions ? adminPaths.alertWebhooks.index() : null,
  );
  const tab = (current: boolean) =>
    ({ current, "aria-current": current ? "page" : undefined }) as const;
  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <Heading>Alerts</Heading>
        <Button outline onClick={() => setDialog({})}>
          Subscribe
        </Button>
      </div>
      <Text className="mt-2 max-w-2xl">
        A signal that stays at its warning or critical level opens an alert. It
        stays open after the signal recovers, until an operator resolves it with
        a comment. Nothing is emailed or sent anywhere until someone subscribes:
        admins to the signals they choose, and webhooks to the signals and
        events an operator set.
      </Text>
      <Navbar aria-label="Alert views" className="mt-4">
        <NavbarSection className="-ml-2">
          <NavbarItem
            href={adminRoutes.alerts.index(key)}
            {...tab(!catalog && !subscriptions)}
          >
            Alerts
          </NavbarItem>
          <NavbarItem
            href={`${adminRoutes.alerts.index(key)}?view=catalog`}
            {...tab(catalog)}
          >
            Catalog
          </NavbarItem>
          <NavbarItem
            href={`${adminRoutes.alerts.index(key)}?view=subscriptions`}
            {...tab(subscriptions)}
          >
            Subscriptions
          </NavbarItem>
        </NavbarSection>
      </Navbar>
      {subscriptions ? (
        <SubscriptionsView
          rules={rules}
          hooks={hooks}
          onChange={(edit) => setDialog({ edit })}
        />
      ) : catalog ? (
        <AlertCatalogView />
      ) : (
        <AlertListView />
      )}
      {dialog && (
        <SubscribeDialog
          key={openedDialog}
          edit={dialog.edit}
          onClose={() => setDialog(undefined)}
          onSaved={() => {
            rules.refresh();
            hooks.refresh();
          }}
        />
      )}
    </>
  );
}

// The time now, read when something happens (an event handler or a state
// initialiser), never while rendering.
function clock(): number {
  return Date.now();
}
const stateLabels: Record<AlertState, string> = {
  open: "Open",
  resolved: "Resolved",
  all: "Open and resolved",
};
const healthLabels = {
  active: "Active",
  confirming: "Confirming recovery",
  recovered: "Recovered, awaiting review",
  unknown: "Unknown",
};
const yesNoLabels = { yes: "Yes", no: "No" };

// The Alerts tab: a search, the time range it opened in, and the filters,
// then the table. All of it is in the URL, so a link or Back returns to it.
function AlertListView() {
  const key = useAdminKey();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const filters = alertListQuery(search);
  // A window ("last 24 hours") is a start taken when it was picked, so the
  // page asks one question until it is changed or refreshed.
  const [asked, setAsked] = useState(clock);
  const list = useAdminResource(
    adminPaths.alerts.index(apiAlertQuery(filters, asked)),
  );
  const [dialog, setDialog] = useState<"filters" | "range" | null>(null);
  const filtersButton = useId();
  const apply = (next: AlertListQuery) => {
    setAsked(clock());
    void navigate(
      adminRoutes.alerts.index(key, { ...next, before: undefined }),
      {
        preventScrollReset: true,
      },
    );
  };
  const badges = alertBadges(filters).filter((b) => b.id !== "opened");
  const hrefFor = (cursor: string | undefined) =>
    adminRoutes.alerts.index(key, { ...filters, before: cursor });
  const range =
    filters.from || filters.to ? "custom" : (filters.window ?? "any");
  return (
    <>
      <div className="mt-6 flex flex-wrap items-center gap-2">
        <div className="min-w-48 max-w-sm flex-1">
          <SearchField
            label="Search alerts"
            value={filters.q ?? ""}
            onSearch={(q) => apply({ ...filters, q: q || undefined })}
          />
        </div>
        <Listbox
          aria-label="Opened"
          value={range}
          onChange={(value) =>
            value === "custom"
              ? setDialog("range")
              : apply({
                  ...filters,
                  from: undefined,
                  to: undefined,
                  window: value === "any" ? undefined : value,
                })
          }
          className="sm:w-60"
        >
          <ListboxOption value="any">
            <ListboxLabel>Any time</ListboxLabel>
          </ListboxOption>
          {alertWindows.map((window) => (
            <ListboxOption key={window} value={window}>
              <ListboxLabel>{windowLabels[window]}</ListboxLabel>
            </ListboxOption>
          ))}
          <ListboxOption value="custom">
            <ListboxLabel>
              {range === "custom" ? openedLabel(filters) : "Custom range…"}
            </ListboxLabel>
          </ListboxOption>
        </Listbox>
        <span id={filtersButton} className="contents">
          <Button outline onClick={() => setDialog("filters")}>
            <FunnelIcon />
            Filters
            {alertBadges(filters).filter(
              (b) => b.id !== "q" && b.id !== "opened",
            ).length > 0 && (
              <Badge>
                {
                  alertBadges(filters).filter(
                    (b) => b.id !== "q" && b.id !== "opened",
                  ).length
                }
                <span className="sr-only"> applied</span>
              </Badge>
            )}
          </Button>
        </span>
      </div>
      {badges.length > 0 && (
        <ul
          aria-label="Applied filters"
          className="mt-4 flex flex-wrap items-center gap-2"
        >
          {badges.map((badge) => (
            <li key={badge.id}>
              <Badge
                onRemove={() => apply(badge.without)}
                removeLabel={`Remove filter: ${badge.text}`}
              >
                {badge.text}
              </Badge>
            </li>
          ))}
          {badges.length >= 2 && (
            <li>
              <TextButton
                className="text-sm/6"
                onClick={() =>
                  apply({
                    window: filters.window,
                    from: filters.from,
                    to: filters.to,
                  })
                }
              >
                Clear all
              </TextButton>
            </li>
          )}
        </ul>
      )}
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      {list.data ? (
        list.data.data.length === 0 ? (
          <EmptyState
            title={
              badges.length > 0 || filters.window || filters.from
                ? "No alerts match"
                : filters.state === "open"
                  ? "No open alerts"
                  : "No alerts"
            }
            description={
              badges.length > 0 || filters.window || filters.from
                ? "Change or clear the filters to see more."
                : "Alerts appear here when a signal stays at its warning or critical level."
            }
          />
        ) : (
          <div className={cx(list.refreshing && "opacity-60")}>
            <Table className="mt-6">
              <TableHead>
                <TableRow>
                  <TableHeader>Alert</TableHeader>
                  <TableHeader>State</TableHeader>
                  <TableHeader>Value</TableHeader>
                  <TableHeader className="max-sm:hidden">Opened</TableHeader>
                  <TableHeader className="max-md:hidden">Open for</TableHeader>
                </TableRow>
              </TableHead>
              <TableBody>
                {list.data.data.map((alert) => (
                  <TableRow key={alert.id}>
                    <TableCell className="whitespace-normal">
                      <TextLink href={adminRoutes.alerts.show(key, alert.id)}>
                        {alert.title}
                      </TextLink>
                    </TableCell>
                    <TableCell>
                      <AlertBadges alert={alert} />
                    </TableCell>
                    <TableCell>
                      {alertValue(alert, alert.lastValue)}
                      {alert.peakValue > alert.lastValue && (
                        <span className="text-zinc-500">
                          {" "}
                          (peak {alertValue(alert, alert.peakValue)})
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="text-zinc-500 max-sm:hidden">
                      {formatDateTime(alert.openedAt)}
                    </TableCell>
                    <TableCell className="text-zinc-500 max-md:hidden">
                      {openFor(alert)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {filters.state !== "open" && (
              <CursorNav
                label="Alert pages"
                next={list.data.next}
                hrefFor={hrefFor}
              />
            )}
          </div>
        )
      ) : (
        list.loading && (
          <div className="mt-10">
            <ListSkeleton label="Loading alerts" />
          </div>
        )
      )}
      {dialog === "filters" && (
        <AlertFiltersDialog
          filters={filters}
          onClose={() => setDialog(null)}
          onApply={(next) => {
            setDialog(null);
            apply(next);
          }}
        />
      )}
      <RangeDialog
        open={dialog === "range"}
        onClose={() => setDialog(null)}
        from={filters.from}
        to={filters.to}
        description="When the alert opened: whole UTC days and times, up to a year back."
        onApply={(from, to) => {
          setDialog(null);
          apply({ ...filters, window: undefined, from, to });
        }}
      />
    </>
  );
}

// The Filters dialog: status, level, recovery state, signals, acknowledged
// and silenced, applied together with the search and range above the list.
function AlertFiltersDialog({
  filters,
  onClose,
  onApply,
}: {
  filters: AlertListQuery;
  onClose: () => void;
  onApply: (filters: AlertListQuery) => void;
}) {
  // The signals to pick from are the catalogue's: one recorded read, made
  // only when the dialog is opened.
  const catalog = useAdminResource(adminPaths.alertRules.index());
  const [values, setValues] = useState<AlertListQuery>(filters);
  const set = (change: Partial<AlertListQuery>) =>
    setValues((v) => ({ ...v, ...change }));
  const options = useMemo(
    () => (catalog.data ? signalOptions(catalog.data.data, false) : []),
    [catalog.data],
  );
  const submit = (e: FormEvent) => {
    e.preventDefault();
    onApply({
      ...values,
      signals: values.signals?.length ? values.signals : undefined,
    });
  };
  return (
    <Dialog open onClose={onClose} size="xl">
      <form onSubmit={submit} aria-label="Filter alerts">
        <DialogTitle>Filter alerts</DialogTitle>
        <DialogBody className="space-y-6">
          <Field>
            <Label>Signals</Label>
            <Combobox
              value={values.signals ?? []}
              onChange={(signals) => set({ signals })}
              options={options}
              placeholder={
                catalog.data
                  ? "Any signal"
                  : catalog.error
                    ? "Unavailable"
                    : "Loading signals…"
              }
              disabled={!catalog.data}
              aria-label="Signals"
            />
          </Field>
          <div className="grid gap-x-4 gap-y-6 sm:grid-cols-2">
            <Field>
              <Label>Status</Label>
              <Listbox
                value={values.state ?? "open"}
                onChange={(state) => set({ state })}
              >
                {alertStates.map((state) => (
                  <ListboxOption key={state} value={state}>
                    <ListboxLabel>{stateLabels[state]}</ListboxLabel>
                  </ListboxOption>
                ))}
              </Listbox>
            </Field>
            <Field>
              <Label>Level</Label>
              <Listbox
                value={values.level ?? "any"}
                onChange={(level) =>
                  set({ level: level === "any" ? undefined : level })
                }
              >
                <ListboxOption value="any">
                  <ListboxLabel>Any level</ListboxLabel>
                </ListboxOption>
                <ListboxOption value="warn">
                  <ListboxLabel>Warning</ListboxLabel>
                </ListboxOption>
                <ListboxOption value="critical">
                  <ListboxLabel>Critical</ListboxLabel>
                </ListboxOption>
              </Listbox>
            </Field>
            <Field>
              <Label>Recovery</Label>
              <Listbox
                value={values.health ?? "any"}
                onChange={(health) =>
                  set({ health: health === "any" ? undefined : health })
                }
              >
                <ListboxOption value="any">
                  <ListboxLabel>Any</ListboxLabel>
                </ListboxOption>
                {alertHealths.map((health) => (
                  <ListboxOption key={health} value={health}>
                    <ListboxLabel>{healthLabels[health]}</ListboxLabel>
                  </ListboxOption>
                ))}
              </Listbox>
            </Field>
            <YesNoField
              label="Acknowledged"
              value={values.acknowledged}
              onChange={(acknowledged) => set({ acknowledged })}
            />
            <YesNoField
              label="Silenced"
              value={values.silenced}
              onChange={(silenced) => set({ silenced })}
            />
          </div>
        </DialogBody>
        <DialogActions>
          <Button
            plain
            onClick={() =>
              setValues({
                window: filters.window,
                from: filters.from,
                to: filters.to,
                q: filters.q,
              })
            }
          >
            Clear all
          </Button>
          <Button plain onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit">Apply filters</Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}

// Any, Yes or No.
function YesNoField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: "yes" | "no" | undefined;
  onChange: (value: "yes" | "no" | undefined) => void;
}) {
  return (
    <Field>
      <Label>{label}</Label>
      <Listbox
        value={value ?? "any"}
        onChange={(v) => onChange(v === "any" ? undefined : v)}
      >
        <ListboxOption value="any">
          <ListboxLabel>Any</ListboxLabel>
        </ListboxOption>
        {(["yes", "no"] as const).map((v) => (
          <ListboxOption key={v} value={v}>
            <ListboxLabel>{yesNoLabels[v]}</ListboxLabel>
          </ListboxOption>
        ))}
      </Listbox>
    </Field>
  );
}

// The Catalog tab: every rule as a collapsible item, with a search and
// filters (kept in the URL, applied here to the rules loaded: it is one read).
function AlertCatalogView() {
  const key = useAdminKey();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const query = catalogQuery(search);
  const catalog = useAdminResource(adminPaths.alertRules.index());
  const [filtering, setFiltering] = useState(false);
  const go = (next: CatalogQuery) => {
    const params = new URLSearchParams({ view: "catalog" });
    for (const [name, value] of Object.entries(next))
      if (value) params.set(name, value);
    void navigate(`${adminRoutes.alerts.index(key)}?${params}`, {
      replace: true,
      preventScrollReset: true,
    });
  };
  const rules = catalog.data?.data ?? [];
  const shown = filterRules(rules, query);
  const badges = catalogBadges(query);
  const pages = [...new Set(rules.map((r) => r.page))].sort();
  return (
    <div className="mt-6">
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-48 max-w-sm flex-1">
          <SearchField
            label="Search the catalog"
            value={query.q ?? ""}
            onSearch={(q) => go({ ...query, q: q || undefined })}
          />
        </div>
        <Button outline onClick={() => setFiltering(true)}>
          <FunnelIcon />
          Filters
          {badges.filter((b) => b.id !== "q").length > 0 && (
            <Badge>
              {badges.filter((b) => b.id !== "q").length}
              <span className="sr-only"> applied</span>
            </Badge>
          )}
        </Button>
      </div>
      {badges.length > 0 && (
        <ul
          aria-label="Applied filters"
          className="mt-4 flex flex-wrap items-center gap-2"
        >
          {badges.map((badge) => (
            <li key={badge.id}>
              <Badge
                onRemove={() => go(badge.without)}
                removeLabel={`Remove filter: ${badge.text}`}
              >
                {badge.text}
              </Badge>
            </li>
          ))}
        </ul>
      )}
      {catalog.error && (
        <Notice variant="error" className="mt-6" onRetry={catalog.refresh}>
          {catalog.error}
        </Notice>
      )}
      {!catalog.data ? (
        catalog.loading && (
          <div className="mt-6">
            <ListSkeleton label="Loading alert catalog" />
          </div>
        )
      ) : shown.length === 0 ? (
        <EmptyState
          title={rules.length === 0 ? "No alert rules" : "No rules match"}
          description={
            rules.length === 0
              ? "Rules are defined in code."
              : "Change or clear the filters to see more."
          }
        />
      ) : (
        <div className="mt-6 divide-y divide-zinc-950/10 border-y border-zinc-950/10 dark:divide-white/10 dark:border-white/10">
          {shown.map((rule) => (
            <CatalogRule key={rule.signal} rule={rule} />
          ))}
        </div>
      )}
      {filtering && (
        <CatalogFiltersDialog
          query={query}
          pages={pages}
          onClose={() => setFiltering(false)}
          onApply={(next) => {
            setFiltering(false);
            go(next);
          }}
        />
      )}
    </div>
  );
}

// One rule, collapsed to its title, its page and whether you're emailed.
function CatalogRule({ rule }: { rule: AlertRule }) {
  return (
    <Disclosure
      title={rule.title}
      summary={
        <>
          <span className="max-sm:hidden">
            {rule.page.charAt(0).toUpperCase() + rule.page.slice(1)}
          </span>
          {rule.preference !== "off" && <Badge color="lime">Subscribed</Badge>}
        </>
      }
    >
      <div className="space-y-2">
        <Text>{rule.meaning}</Text>
        <Text>
          Signal: {rule.signal}. Raw warning: {rule.warn || "none"}; critical:{" "}
          {rule.critical || "none"} {rule.unit}. Minimum sample:{" "}
          {rule.minSample}; window: {rule.windowSeconds} seconds.
        </Text>
        <Text>
          Alert levels: {rule.levels.join(", ") || "none"}; from {rule.from},
          capped to {rule.cap}. Sustained for {rule.forSeconds} seconds; checked
          every {rule.cadenceSeconds} seconds.
        </Text>
        <Text>
          Recovery: {rule.recoveryChecks} healthy checks
          {rule.recoverySeconds > 0
            ? ` spanning ${rule.recoverySeconds} seconds`
            : " at the next successful hourly check"}
          ; recovery awaits operator resolution.
        </Text>
        <Text>Your email subscription: {preferenceText(rule)}.</Text>
        {rule.ineffective && (
          <Notice variant="warning">
            You subscribed to critical alerts only, but this rule never alerts
            above warning, so it will never email you.
          </Notice>
        )}
      </div>
    </Disclosure>
  );
}

function CatalogFiltersDialog({
  query,
  pages,
  onClose,
  onApply,
}: {
  query: CatalogQuery;
  pages: string[];
  onClose: () => void;
  onApply: (query: CatalogQuery) => void;
}) {
  const [values, setValues] = useState<CatalogQuery>(query);
  const set = (change: Partial<CatalogQuery>) =>
    setValues((v) => ({ ...v, ...change }));
  return (
    <Dialog open onClose={onClose} size="xl">
      <form
        aria-label="Filter the catalog"
        onSubmit={(e) => {
          e.preventDefault();
          onApply(values);
        }}
      >
        <DialogTitle>Filter the catalog</DialogTitle>
        <DialogBody>
          <div className="grid gap-x-4 gap-y-6 sm:grid-cols-2">
            <Field>
              <Label>Page</Label>
              <Listbox
                value={values.page ?? "any"}
                onChange={(page) =>
                  set({ page: page === "any" ? undefined : page })
                }
              >
                <ListboxOption value="any">
                  <ListboxLabel>Any page</ListboxLabel>
                </ListboxOption>
                {pages.map((page) => (
                  <ListboxOption key={page} value={page}>
                    <ListboxLabel>
                      {page.charAt(0).toUpperCase() + page.slice(1)}
                    </ListboxLabel>
                  </ListboxOption>
                ))}
              </Listbox>
            </Field>
            <Field>
              <Label>Can alert at</Label>
              <Listbox
                value={values.level ?? "any"}
                onChange={(level) =>
                  set({ level: level === "any" ? undefined : level })
                }
              >
                <ListboxOption value="any">
                  <ListboxLabel>Any level</ListboxLabel>
                </ListboxOption>
                <ListboxOption value="warn">
                  <ListboxLabel>Warning</ListboxLabel>
                </ListboxOption>
                <ListboxOption value="critical">
                  <ListboxLabel>Critical</ListboxLabel>
                </ListboxOption>
              </Listbox>
            </Field>
            <Field>
              <Label>Your subscription</Label>
              <Listbox
                value={values.subscribed ?? "any"}
                onChange={(subscribed) =>
                  set({
                    subscribed: subscribed === "any" ? undefined : subscribed,
                  })
                }
              >
                <ListboxOption value="any">
                  <ListboxLabel>Any</ListboxLabel>
                </ListboxOption>
                <ListboxOption value="yes">
                  <ListboxLabel>Subscribed</ListboxLabel>
                </ListboxOption>
                <ListboxOption value="no">
                  <ListboxLabel>Not subscribed</ListboxLabel>
                </ListboxOption>
              </Listbox>
            </Field>
            <Field>
              <Label>Checked</Label>
              <Listbox
                value={values.cadence ?? "any"}
                onChange={(cadence) =>
                  set({ cadence: cadence === "any" ? undefined : cadence })
                }
              >
                <ListboxOption value="any">
                  <ListboxLabel>Any</ListboxLabel>
                </ListboxOption>
                <ListboxOption value="minute">
                  <ListboxLabel>Each minute</ListboxLabel>
                </ListboxOption>
                <ListboxOption value="hourly">
                  <ListboxLabel>Hourly</ListboxLabel>
                </ListboxOption>
              </Listbox>
            </Field>
          </div>
        </DialogBody>
        <DialogActions>
          <Button plain onClick={() => setValues({ q: query.q })}>
            Clear all
          </Button>
          <Button plain onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit">Apply filters</Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}

function preferenceText(rule: AlertRule): string {
  if (rule.preference === "off") return "none (no email)";
  return rule.preference === "critical"
    ? "critical alerts only"
    : "warnings and critical alerts";
}

const eventLabels: Record<AlertWebhookEvent, string> = {
  opened: "Opened",
  escalated: "Escalated to critical",
  recovered: "Recovery confirmed",
  reopened: "Reopened",
  resolved: "Resolved by an operator",
  reminded: "Reminder",
};

// Your own alert email, and the shared webhooks and heartbeats: what each
// receives, and its latest delivery. Reading is any admin's; changing a
// webhook is an operator's.
function SubscriptionsView({
  rules,
  hooks,
  onChange,
}: {
  rules: ReturnType<typeof useAdminResource<AlertCatalog>>;
  hooks: ReturnType<typeof useAdminResource<{ data: AlertWebhook[] }>>;
  onChange: (edit: AlertWebhook | "email") => void;
}) {
  const api = useAdminApi();
  const operator = adminCan(useAdminSession().role, "operator");
  const [removing, setRemoving] = useState<AlertWebhook | null>(null);
  const opened = useOpenCount(removing !== null);
  const mine = rules.data?.data.filter((r) => r.preference !== "off") ?? [];
  return (
    <div className="mt-6 space-y-12">
      <section aria-labelledby="my-email">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <Heading level={2} id="my-email">
            Your email
          </Heading>
          <Button outline onClick={() => onChange("email")}>
            Edit
          </Button>
        </div>
        {rules.error && (
          <Notice variant="error" className="mt-4" onRetry={rules.refresh}>
            {rules.error}
          </Notice>
        )}
        {!rules.data ? (
          rules.loading && (
            <div className="mt-4">
              <ListSkeleton label="Loading your subscriptions" />
            </div>
          )
        ) : mine.length === 0 ? (
          <Text className="mt-2">
            You aren&rsquo;t subscribed to any alert email, so you won&rsquo;t
            get any. Choose signals with Subscribe.
          </Text>
        ) : (
          <Table className="mt-4">
            <TableHead>
              <TableRow>
                <TableHeader>Signal</TableHeader>
                <TableHeader>Emailed at</TableHeader>
              </TableRow>
            </TableHead>
            <TableBody>
              {mine.map((rule) => (
                <TableRow key={rule.signal}>
                  <TableCell className="whitespace-normal">
                    {rule.title}
                  </TableCell>
                  <TableCell className="text-zinc-500">
                    {rule.preference === "critical"
                      ? "Critical only"
                      : "Warnings and critical"}
                    {rule.ineffective && (
                      <Badge color="rose" className="ml-2">
                        Never alerts at critical
                      </Badge>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </section>
      <section aria-labelledby="webhooks">
        <Heading level={2} id="webhooks">
          Webhooks and heartbeats
        </Heading>
        <Text className="mt-2 max-w-2xl">
          Shared by every admin and managed by operators. A webhook receives
          only the signals and events it was set to; a heartbeat is pinged every
          minute while alerts are being evaluated.
        </Text>
        {hooks.error && (
          <Notice variant="error" className="mt-4" onRetry={hooks.refresh}>
            {hooks.error}
          </Notice>
        )}
        {!hooks.data ? (
          hooks.loading && (
            <div className="mt-4">
              <ListSkeleton label="Loading webhooks" />
            </div>
          )
        ) : hooks.data.data.length === 0 ? (
          <EmptyState
            title="No webhooks or heartbeats"
            description={
              operator ? "Add one with Subscribe." : "An operator can add one."
            }
          />
        ) : (
          <Table className="mt-4">
            <TableHead>
              <TableRow>
                <TableHeader>Name</TableHeader>
                <TableHeader>Receives</TableHeader>
                <TableHeader className="max-sm:hidden">
                  Last delivery
                </TableHeader>
                {operator && <TableHeader className="w-0" />}
              </TableRow>
            </TableHead>
            <TableBody>
              {hooks.data.data.map((hook) => (
                <TableRow key={hook.id}>
                  <TableCell className="whitespace-normal">
                    <span className="font-medium">{hook.name}</span>{" "}
                    <Badge>
                      {hook.kind === "webhook" ? "Webhook" : "Heartbeat"}
                    </Badge>
                    {!hook.enabled && (
                      <Badge color="rose" className="ml-1">
                        Paused
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="whitespace-normal text-zinc-500">
                    <Receives hook={hook} />
                  </TableCell>
                  <TableCell className="text-zinc-500 max-sm:hidden">
                    <LastDelivery hook={hook} />
                  </TableCell>
                  {operator && (
                    <TableCell className="w-0 text-right">
                      <Dropdown>
                        <DropdownButton
                          plain
                          aria-label={`Actions for ${hook.name}`}
                        >
                          <EllipsisHorizontalIcon />
                        </DropdownButton>
                        <DropdownMenu anchor="bottom end">
                          <DropdownItem onClick={() => onChange(hook)}>
                            <DropdownLabel>Edit</DropdownLabel>
                          </DropdownItem>
                          <DropdownItem
                            variant="danger"
                            onClick={() => setRemoving(hook)}
                          >
                            <DropdownLabel>Delete</DropdownLabel>
                          </DropdownItem>
                        </DropdownMenu>
                      </Dropdown>
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </section>
      {removing && (
        <AdminActionDialog
          key={opened}
          open
          onClose={() => setRemoving(null)}
          title={`Delete “${removing.name}”?`}
          description="Notices queued for it are dropped, and it stops receiving alerts."
          confirm="Delete"
          variant="danger"
          action={(reason) => api.alertWebhooks().destroy(removing.id, reason)}
          onDone={() => {
            toast.success(`${removing.name} deleted.`);
            setRemoving(null);
            void hooks.refresh();
          }}
        />
      )}
    </div>
  );
}

function Receives({ hook }: { hook: AlertWebhook }) {
  if (hook.kind === "heartbeat")
    return <>A ping each minute while alerts are evaluated</>;
  if (hook.signals.length === 0 || hook.events.length === 0)
    return <Badge color="rose">Nothing selected: sends nothing</Badge>;
  const noun = (n: number, one: string) => `${n} ${one}${n === 1 ? "" : "s"}`;
  return (
    <>
      {noun(hook.signals.length, "signal")},{" "}
      {hook.events.map((e) => eventLabels[e].toLowerCase()).join(", ")}
    </>
  );
}

function LastDelivery({ hook }: { hook: AlertWebhook }) {
  if (hook.kind === "heartbeat" || !hook.lastDelivery) return <>—</>;
  const { status, at } = hook.lastDelivery;
  return (
    <>
      {status === "delivered" && <Badge color="lime">Delivered</Badge>}
      {status === "failed" && <Badge color="red">Failed</Badge>}
      {status === "pending" && <Badge>Waiting</Badge>}
      {at && <span className="ml-2">{formatDateTime(at)}</span>}
    </>
  );
}

// The one dialog: a Type, then what to receive, then that type's own
// settings, all live. Email is your own inbox (no code); a webhook or
// heartbeat is shared infrastructure, so saving it asks an operator for a
// reason and a fresh code.
function SubscribeDialog({
  edit,
  onClose,
  onSaved,
}: {
  edit?: AlertWebhook | "email";
  onClose: () => void;
  onSaved: () => void;
}) {
  const catalog = useAdminResource(adminPaths.alertRules.index());
  if (catalog.data)
    return (
      <SubscribeForm
        catalog={catalog.data}
        edit={edit}
        onClose={onClose}
        onSaved={onSaved}
      />
    );
  return (
    <Dialog open onClose={onClose} size="xl">
      <DialogTitle>Subscribe to alerts</DialogTitle>
      <DialogBody>
        {catalog.error && (
          <Notice variant="error" onRetry={catalog.refresh}>
            {catalog.error}
          </Notice>
        )}
        {catalog.loading && <ListSkeleton label="Loading alert signals" />}
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose}>
          Close
        </Button>
      </DialogActions>
    </Dialog>
  );
}

type SubscriptionType = "email" | "webhook" | "heartbeat";
const typeLabels: Record<SubscriptionType, string> = {
  email: "Email",
  webhook: "Webhook",
  heartbeat: "Heartbeat",
};

// The signals as a Combobox tree: grouped by the monitoring page they belong
// to, as the Events filters group their events. With `withEvents`, the kinds
// of alert event follow as a last group of their own, so one box picks both.
const eventPrefix = "event:";
function signalOptions(
  rules: AlertRule[],
  withEvents: boolean,
): ComboboxNode[] {
  const pages = new Map<string, ComboboxNode>();
  for (const rule of rules) {
    let group = pages.get(rule.page);
    if (!group) {
      const label = rule.page.charAt(0).toUpperCase() + rule.page.slice(1);
      group = { value: `page:${rule.page}`, label, text: label, children: [] };
      pages.set(rule.page, group);
    }
    group.children!.push({
      value: rule.signal,
      label: rule.title,
      text: `${rule.title} ${rule.signal}`,
      chip: rule.title,
    });
  }
  const options = [...pages.values()];
  if (withEvents)
    options.push({
      value: "events",
      label: "Alert events",
      text: "Alert events",
      children: alertWebhookEvents.map((event) => ({
        value: eventPrefix + event,
        label: eventLabels[event],
        text: eventLabels[event],
      })),
    });
  return options;
}
// What the one box holds, as signals and as event kinds.
function selection(signals: string[], kinds: string[]): string[] {
  return [...signals, ...kinds.map((k) => eventPrefix + k)];
}
function splitSelection(values: string[]): {
  signals: string[];
  kinds: AlertWebhookEvent[];
} {
  return {
    signals: values.filter((v) => !v.startsWith(eventPrefix)),
    kinds: values
      .filter((v) => v.startsWith(eventPrefix))
      .map((v) => v.slice(eventPrefix.length) as AlertWebhookEvent),
  };
}

function SubscribeForm({
  catalog,
  edit,
  onClose,
  onSaved,
}: {
  catalog: AlertCatalog;
  edit?: AlertWebhook | "email";
  onClose: () => void;
  onSaved: () => void;
}) {
  const api = useAdminApi();
  const session = useAdminSession();
  const operator = adminCan(session.role, "operator");
  // The webhook being changed: the one opened, or the one just created.
  const [hook, setHook] = useState<AlertWebhook | undefined>(
    edit && edit !== "email" ? edit : undefined,
  );
  const subscribed = catalog.data.filter((r) => r.preference !== "off");
  const [type, setType] = useState<SubscriptionType>(hook?.kind ?? "email");
  const [name, setName] = useState(hook?.name ?? "");
  // Email and webhooks each keep their own choices, so what you picked for
  // your inbox is never sent to a new webhook by switching the Subscription.
  const [emailPicked, setEmailPicked] = useState<string[]>(() =>
    selection(
      subscribed.map((r) => r.signal),
      subscribed.length > 0
        ? alertWebhookEvents.filter((kind) =>
            subscribed.some((r) => r.events.includes(kind)),
          )
        : [...alertWebhookEvents],
    ),
  );
  const [hookPicked, setHookPicked] = useState<string[]>(() =>
    selection(hook?.signals ?? [], hook?.events ?? []),
  );
  const [level, setLevel] = useState<"warn" | "critical">(
    subscribed.length > 0 &&
      subscribed.every((r) => r.preference === "critical")
      ? "critical"
      : "warn",
  );
  const [url, setUrl] = useState("");
  const [format, setFormat] = useState<AlertWebhookFormat>(
    hook?.format === "slack" ? "slack" : "json",
  );
  const [enabled, setEnabled] = useState(hook?.enabled ?? true);
  const [confirming, setConfirming] = useState(false);
  const opened = useOpenCount(confirming);
  const shared = type !== "email";
  const options = useMemo(
    () => signalOptions(catalog.data, true),
    [catalog.data],
  );
  const picked = shared ? hookPicked : emailPicked;
  const setPicked = shared ? setHookPicked : setEmailPicked;
  const { signals, kinds } = splitSelection(picked);
  const fixed = hook !== undefined;
  const save = useAction(saveEmail, {
    onError: (e) => {
      toast.error(errorMessage(e));
      return true;
    },
  });
  // Your own subscriptions, as a patch: each signal picked, at the level and
  // for the events chosen (when either changed), and any left off.
  async function saveEmail() {
    const before = new Map(subscribed.map((r) => [r.signal, r]));
    const same = (a: string[], b: string[]) =>
      a.length === b.length && a.every((k) => b.includes(k));
    const patch = [
      ...signals
        .filter((signal) => {
          const old = before.get(signal);
          return !old || old.preference !== level || !same(old.events, kinds);
        })
        .map((signal) => ({ signal, minLevel: level, events: kinds })),
      ...[...before.keys()]
        .filter((signal) => !signals.includes(signal))
        .map((signal) => ({ signal, minLevel: "off" as const })),
    ];
    if (patch.length > 0) await api.alertSubscriptions().update(patch);
    toast.success("Your email subscription is saved.");
    onSaved();
    onClose();
  }
  // A webhook or heartbeat: only what changed, on an edit.
  function input(reason: string): AlertWebhookInput {
    const out: AlertWebhookInput = { reason, name: name.trim() };
    if (url.trim() !== "") out.url = url.trim();
    if (type === "webhook") {
      out.format = format;
      out.signals = signals;
      out.events = kinds;
    }
    out.enabled = enabled;
    return out;
  }
  async function saveHook(reason: string) {
    if (hook) {
      const updated = await api
        .alertWebhooks()
        .update(hook.id, hook.revision, input(reason));
      setHook(updated);
      toast.success(`${updated.name} saved.`);
      onSaved();
      onClose();
      return;
    }
    const created = await api
      .alertWebhooks()
      .store(type as "webhook" | "heartbeat", input(reason));
    // The webhook now exists: stay, so its signing key can be copied.
    setHook(created);
    setUrl("");
    toast.success(`${created.name} added.`);
    onSaved();
    if (created.kind === "heartbeat") onClose();
  }
  const ready = !shared || (name.trim() !== "" && (fixed || url.trim() !== ""));
  return (
    <Dialog open onClose={onClose} size="xl">
      <DialogTitle>
        {hook && type === hook.kind
          ? `Change “${hook.name}”`
          : "Subscribe to alerts"}
      </DialogTitle>
      <DialogBody>
        <FieldGroup>
          <Field>
            <Label>Subscription</Label>
            <Listbox
              value={type}
              disabled={fixed}
              onChange={(value) => setType(value as SubscriptionType)}
            >
              {(["email", "webhook", "heartbeat"] as const)
                .filter((t) => t === "email" || operator)
                .map((t) => (
                  <ListboxOption key={t} value={t}>
                    <ListboxLabel>{typeLabels[t]}</ListboxLabel>
                  </ListboxOption>
                ))}
            </Listbox>
          </Field>
          {type === "email" && (
            <Field locked="Your sign-in address. Change it in Account settings.">
              <Label>Recipient</Label>
              <Input value={session.user.email} readOnly />
            </Field>
          )}
          {shared && (
            <Field>
              <Label>Name</Label>
              <Input
                name="name"
                autoComplete="off"
                maxLength={80}
                value={name}
                onChange={(e) => setName(e.currentTarget.value)}
              />
            </Field>
          )}
          {type !== "heartbeat" && (
            <Field>
              <Label>Events</Label>
              <Combobox
                value={picked}
                onChange={setPicked}
                options={options}
                placeholder="Search signals and events"
                aria-label="Events"
              />
            </Field>
          )}
          {type === "email" && (
            <RadioGroup
              label="Emailed at"
              value={level}
              onChange={setLevel}
              options={[
                { value: "warn", label: "Warnings and critical alerts" },
                { value: "critical", label: "Critical alerts only" },
              ]}
            />
          )}
          {shared && (
            <Field>
              <Label>URL</Label>
              <Input
                type="url"
                name="url"
                autoComplete="off"
                placeholder={
                  fixed ? "Leave blank to keep the current URL" : "https://"
                }
                value={url}
                onChange={(e) => setUrl(e.currentTarget.value)}
              />
            </Field>
          )}
          {type === "webhook" && (
            <Field>
              <Label>Format</Label>
              <Listbox
                value={format}
                onChange={(value) => setFormat(value as AlertWebhookFormat)}
              >
                <ListboxOption value="json">
                  <ListboxLabel>JSON</ListboxLabel>
                </ListboxOption>
                <ListboxOption value="slack">
                  <ListboxLabel>Slack</ListboxLabel>
                </ListboxOption>
              </Listbox>
            </Field>
          )}
          {shared && (
            <Checkbox checked={enabled} onChange={setEnabled}>
              Enabled
            </Checkbox>
          )}
          {type === "webhook" && hook && (
            <SigningKeyField
              webhook={hook}
              onRotated={setHook}
              onRefresh={onSaved}
            />
          )}
          {type === "webhook" && (
            <WebhookExamples format={format} kinds={kinds} />
          )}
          {type === "heartbeat" && <HeartbeatExplanation />}
          <ErrorNotice error={save.error} />
        </FieldGroup>
      </DialogBody>
      <DialogActions>
        <Button plain onClick={onClose}>
          {hook ? "Close" : "Cancel"}
        </Button>
        {shared ? (
          <Button
            disabled={!ready}
            disabledReason="Fill in required fields"
            onClick={() => setConfirming(true)}
          >
            {hook ? "Save changes" : `Add ${typeLabels[type].toLowerCase()}`}
          </Button>
        ) : (
          <Button loading={save.loading} onClick={() => void save.run()}>
            Save
          </Button>
        )}
      </DialogActions>
      {confirming && (
        <AdminActionDialog
          key={opened}
          open
          onClose={() => setConfirming(false)}
          title={hook ? `Save “${hook.name}”?` : `Add this ${type}?`}
          description={
            type === "webhook"
              ? "A webhook receives alert details, so this needs a reason and a fresh code. Changing the URL or format drops its unsent notices."
              : "A heartbeat sends a request from this server, so this needs a reason and a fresh code."
          }
          confirm={hook ? "Save" : "Add"}
          action={saveHook}
          onDone={() => setConfirming(false)}
        />
      )}
    </Dialog>
  );
}

// A webhook's own signing key, read-only with a lock and Copy like an ID:
// shown on request, since reading it asks an operator for a fresh code.
// Rotating gives a new key and keeps the old one signing for 15 minutes.
function SigningKeyField({
  webhook,
  onRotated,
  onRefresh,
}: {
  webhook: AlertWebhook;
  onRotated: (webhook: AlertWebhook) => void;
  onRefresh: () => void;
}) {
  const api = useAdminApi();
  const [shown, setShown] = useState<{ key: string; keyId: string } | null>(
    null,
  );
  const [needsCode, setNeedsCode] = useState(false);
  const [code, setCode] = useState("");
  const [rotating, setRotating] = useState(false);
  const opened = useOpenCount(rotating);
  const read = useAction(
    async () => {
      if (needsCode) {
        await api.stepUp(code);
        setNeedsCode(false);
        setCode("");
      }
      setShown(await api.alertWebhookSigningKey(webhook.id).show());
    },
    {
      onError: (error) => {
        if (needsStepUp(error)) {
          setNeedsCode(true);
          setCode("");
          return true;
        }
        return false;
      },
    },
  );
  const copy = useCopy(shown?.key ?? "");
  return (
    <div className="space-y-3">
      {webhook.overlapUntil && (
        <Notice variant="info">
          The previous key also signs until{" "}
          {formatDateTime(webhook.overlapUntil)}.
        </Notice>
      )}
      <Field
        locked={`Key ID ${shown?.keyId ?? webhook.keyId}. Generated for this webhook; rotate it to replace it.`}
      >
        <Label>Signing key</Label>
        {shown ? (
          <InputGroup>
            <Input
              value={shown.key}
              className="font-mono"
              onFocus={(e) => e.currentTarget.select()}
            />
            <InputActions>
              <CopyButton copy={copy} label="Copy signing key" />
            </InputActions>
          </InputGroup>
        ) : (
          <InputGroup>
            <Input
              value="••••••••••••••••••••••••••••••••"
              className="font-mono"
            />
          </InputGroup>
        )}
      </Field>
      <CopyFailure copy={copy}>Select the key and copy it instead.</CopyFailure>
      <ErrorNotice error={read.error} />
      {needsCode && !shown && (
        <AppOrRecoveryCode
          description="Enter a code to read the key. It lets you take actions for the next 5 minutes."
          value={code}
          onChange={setCode}
          error={read.error.message}
          disabled={read.busy}
          autoFocus
        />
      )}
      <div className="flex flex-wrap gap-3">
        {shown ? (
          <Button outline onClick={() => setShown(null)}>
            Hide key
          </Button>
        ) : (
          <Button
            outline
            loading={read.loading}
            disabled={needsCode && code.trim() === ""}
            disabledReason="Enter a code"
            onClick={() => void read.run()}
          >
            Show signing key
          </Button>
        )}
        <Button outline onClick={() => setRotating(true)}>
          Rotate key
        </Button>
      </div>
      {rotating && (
        <AdminActionDialog
          key={opened}
          open
          onClose={() => setRotating(false)}
          title="Rotate the signing key?"
          description="A new key signs from now. For 15 minutes each post carries a second signature made with the old key, so the receiver can switch over without dropping notices."
          confirm="Rotate key"
          action={async (reason) => {
            const updated = await api
              .alertWebhookSigningKey(webhook.id)
              .store(webhook.revision, reason);
            onRotated(updated);
            setShown(await api.alertWebhookSigningKey(webhook.id).show());
          }}
          onDone={() => {
            setRotating(false);
            toast.success("Signing key rotated.");
            onRefresh();
          }}
        />
      )}
    </div>
  );
}

// What a heartbeat is, in words.
function HeartbeatExplanation() {
  return (
    <Notice variant="info">
      This server asks the URL (a GET, with nothing in it) every minute, but
      only after alerts were evaluated successfully. Point it at a service such
      as Healthchecks.io or Better Stack and have it alert you when the pings
      stop: that catches every server being down, the database being
      unreachable, or the alert loop being stuck, which nothing inside the app
      can report.
    </Notice>
  );
}

// An example of what a webhook receives, for each event it chose (all of them
// when none is chosen yet), in its format, with made-up values. Collapsed
// until asked for.
function WebhookExamples({
  format,
  kinds,
}: {
  format: AlertWebhookFormat;
  kinds: AlertWebhookEvent[];
}) {
  const shown = kinds.length > 0 ? kinds : [...alertWebhookEvents];
  return (
    <Disclosure title="Example payloads and how to verify them" bordered>
      <div className="divide-y divide-zinc-950/10 border-t border-zinc-950/10 dark:divide-white/10 dark:border-white/10">
        {shown.map((kind) => (
          <Disclosure key={kind} title={eventLabels[kind]}>
            <CodeBlock>{examplePayload(kind, format)}</CodeBlock>
          </Disclosure>
        ))}
        <Disclosure title="Retries and ordering">
          <div className="space-y-2 text-sm/6 text-zinc-600 dark:text-zinc-400">
            <p>
              Delivery is at least once and not ordered. Ignore a delivery ID
              you have seen, and a <code>sequence</code> lower than the last you
              saw for that alert. Slack&rsquo;s webhooks don&rsquo;t verify the
              signature.
            </p>
          </div>
        </Disclosure>
      </div>
      <div className="mt-4 space-y-2 text-sm/6">
        <p className="font-medium text-zinc-950 dark:text-white">
          Headers on every post
        </p>
        <CodeBlock>{exampleHeaders}</CodeBlock>
      </div>
      <div className="mt-4 space-y-2 text-sm/6">
        <p className="font-medium text-zinc-950 dark:text-white">
          Verifying a post
        </p>
        <ol className="list-decimal space-y-2 pl-5 text-zinc-600 dark:text-zinc-400">
          <li>
            Take the <code>t=</code> value from{" "}
            <code>X-Lewcontext-Signature</code> (a Unix time in seconds) and the
            raw body, exactly as received.
          </li>
          <li>
            Compute HMAC-SHA256 of <code>t + &quot;.&quot; + body</code> with
            this webhook&rsquo;s signing key, and compare the hex result with
            each <code>v1=</code> value in constant time.
          </li>
          <li>
            Refuse the post if no signature matches, or if the timestamp is more
            than five minutes old.
          </li>
          <li>
            Check <code>X-Lewcontext-Key-Id</code> to pick the right key when
            your receiver holds several. For 15 minutes after a rotation there
            are two <code>v1=</code> signatures, one per key: accept either.
          </li>
        </ol>
      </div>
    </Disclosure>
  );
}

function CodeBlock({ children }: { children: string }) {
  return (
    <pre className="mt-2 overflow-x-auto rounded-lg bg-zinc-950/5 p-4 font-mono text-xs/5 text-zinc-800 dark:bg-white/5 dark:text-zinc-200">
      <code>{children}</code>
    </pre>
  );
}

const exampleHeaders = `Content-Type: application/json
X-Lewcontext-Delivery: evt_Xk3p9QwLm2RnTa
X-Lewcontext-Key-Id: key_Hq4Zr8Tn
X-Lewcontext-Signature: t=1793543400,v1=5f2c…e91a`;

// A made-up payload for an alert event, as the API sends it.
function examplePayload(
  kind: AlertWebhookEvent,
  format: AlertWebhookFormat,
): string {
  const times = {
    openedAt: "2026-11-02T12:00:00Z",
    recoveredAt: null as string | null,
    resolvedAt: null as string | null,
  };
  const state = {
    opened: "active",
    escalated: "active",
    recovered: "recovered",
    reopened: "active",
    resolved: "resolved",
    reminded: "active",
  }[kind];
  if (kind === "recovered" || kind === "resolved")
    times.recoveredAt = "2026-11-02T12:25:00Z";
  if (kind === "resolved") times.resolvedAt = "2026-11-02T12:40:00Z";
  const occurredAt = times.resolvedAt ?? times.recoveredAt ?? times.openedAt;
  const title = "Latency, 95th percentile";
  if (format === "slack") {
    const what = {
      opened: "Alert opened",
      escalated: "Now critical",
      recovered: "Recovery confirmed; awaiting review",
      reopened: "Alert reopened",
      resolved: "Resolved after recovery",
      reminded: "Still active",
    }[kind];
    return JSON.stringify(
      {
        text: `${what} at 2 Nov 2026, ${occurredAt.slice(11, 16)} UTC: ${title}. Value 3200 ms. State ${state}; sequence 1.`,
      },
      null,
      2,
    );
  }
  return JSON.stringify(
    {
      version: 2,
      alert: "alt_Xk3p9QwLm2RnTa",
      cycle: "inc_Hq4Zr8TnVb6Lc1",
      sequence: 1,
      event: `alert.${kind}`,
      ...(kind === "reopened" ? { cause: "relapse" } : {}),
      ...(kind === "resolved" ? { source: "admin" } : {}),
      signal: "http.latency_p95",
      level: kind === "escalated" ? "critical" : "warn",
      value: kind === "recovered" || kind === "resolved" ? 0 : 3200,
      occurredAt,
      state,
      currentLevel: kind === "recovered" || kind === "resolved" ? "ok" : "warn",
      rawLevel: kind === "recovered" || kind === "resolved" ? "ok" : "warn",
      title,
      unit: "ms",
      meaning: "Requests are slow.",
      ...times,
      healthySince: kind === "recovered" ? "2026-11-02T12:15:00Z" : null,
      healthyChecks: kind === "recovered" ? 11 : 0,
    },
    null,
    2,
  );
}
