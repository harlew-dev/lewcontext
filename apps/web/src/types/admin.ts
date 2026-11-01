import type { Sort } from "@/lib/pagination";
import type { User } from "@/lib/types";
import type { Role } from "./workspaces";
// What the admin dashboard shows (docs/api/admin.md): every account and every
// workspace, the audit trail and the mail queue.

// What an admin may do (docs/api/admin.md), least access first: a viewer
// reads every admin page, an operator may also act. Set from the command
// line only; the API refuses an action to a role without it (403), and the
// pages hide what the role can't do (adminCan).
export const adminRoles = ["viewer", "operator"] as const;
export type AdminRole = (typeof adminRoles)[number];
// The signed-in admin's session, as /api/admin/me answers.
export type AdminSession = {
  user: User;
  role: AdminRole;
  csrfToken: string;
  expiresAt: string;
  // Which alerts the admin is emailed, and how many alerts were open when
  // the page loaded (the sidebar's badge).
  alertEmail: AlertEmail;
  openAlerts: number;
};
export const alertEmails = ["all", "critical", "none"] as const;
export type AlertEmail = (typeof alertEmails)[number];
// An admin as the Admins page lists them: their live admin sessions,
// and lastSignedInAt, their newest admin sign-in still in the audit trail
// (events are kept 90 days by default).
export type AdminAccountSession = {
  createdAt: string;
  lastSeenAt: string;
  ip: string;
  userAgent: string;
  // The session asking.
  current: boolean;
  // Made under the key being retired (ADMIN_ROUTE_KEY_PREVIOUS).
  previousKey: boolean;
};
export type AdminAccount = {
  user: AdminPerson;
  role: AdminRole;
  mfaEnabled: boolean;
  // While set and ahead, they may still set MFA up at the door.
  mfaSetupUntil: string | null;
  alertEmail: AlertEmail;
  grantedAt: string;
  lastSignedInAt: string | null;
  sessions: AdminAccountSession[];
};
export type AdminUser = {
  id: string;
  name: string;
  email: string;
  createdAt: string;
  // Null for an account that never verified its email.
  emailVerifiedAt: string | null;
  mfaEnabled: boolean;
  admin: boolean;
  workspaceCount: number;
  lastSignedInAt: string | null;
  // An admin suspended it: it can't sign in (AdminUserDetail.suspension
  // says why).
  suspended: boolean;
};
export type AdminMembership = {
  workspace: { id: string; name: string };
  role: Role;
  addedAt: string;
};
// Why an account is suspended, by whom (null from the command line, or once
// that admin's account is gone) and since when. Shown to admins only.
export type AdminSuspension = {
  reason: string;
  suspendedBy: AdminPerson | null;
  suspendedAt: string;
};
export type AdminUserDetail = AdminUser & {
  memberships: AdminMembership[];
  sessionCount: number;
  suspension: AdminSuspension | null;
};
export type AdminPerson = { id: string; name: string; email: string };
export type AdminWorkspace = {
  id: string;
  name: string;
  createdAt: string;
  memberCount: number;
  projectCount: number;
  // Up to three, oldest first.
  owners: AdminPerson[];
};
export type AdminMember = AdminPerson & { role: Role; addedAt: string };
export type AdminWorkspaceDetail = AdminWorkspace & {
  members: AdminMember[];
  // Live invitations: what Revoke pending invitations would revoke.
  pendingInvitations: number;
};
// Sorts the lists accept; "-field" sorts descending. Mirror adminUserSorts
// and adminWorkspaceSorts in apps/api/internal/store/admin_views.go.
export const adminUserSortable = ["created", "name", "email"] as const;
export type AdminUserSort = Sort<(typeof adminUserSortable)[number]>;
export const defaultAdminUserSort: AdminUserSort = "-created";
export const adminWorkspaceSortable = ["created", "name"] as const;
export type AdminWorkspaceSort = Sort<(typeof adminWorkspaceSortable)[number]>;
export const defaultAdminWorkspaceSort: AdminWorkspaceSort = "-created";
// A blocked address as the Email blocks page lists it: when it was blocked,
// and when the newest block link sent to it stops working (null when none
// works any more, so only an admin or the operator can unblock it).
export type AdminEmailBlock = {
  email: string;
  createdAt: string;
  linkExpiresAt: string | null;
};
// Mirrors adminEmailBlockSorts in apps/api/internal/store/admin_email_blocks.go.
export const adminEmailBlockSortable = ["created", "email"] as const;
export type AdminEmailBlockSort = Sort<
  (typeof adminEmailBlockSortable)[number]
>;
export const defaultAdminEmailBlockSort: AdminEmailBlockSort = "-created";
// A list page's state, kept in its URL.
export type AdminListQuery<S> = { page: number; q: string; sort: S };
// A client address or range every server refuses (403) until expiresAt,
// as CIDR ("203.0.113.7/32"); createdBy is null once that admin's account
// is gone.
export type AdminIPBlock = {
  id: string;
  network: string;
  reason: string;
  createdBy: AdminPerson | null;
  createdAt: string;
  expiresAt: string;
};
// How long a new block lasts: at most 30 days. Mirrors ipBlockLifetimes in
// apps/api/internal/httpapi/endpoints/admin/ip_blocks.go.
export const ipBlockLifetimes = ["1h", "24h", "7d", "30d"] as const;
export type IPBlockLifetime = (typeof ipBlockLifetimes)[number];

// An account or workspace named inside an event: name (and an account's
// email) are missing once it's deleted, leaving the ID from the payload.
export type AdminRef = { id: string; name?: string; email?: string };
// An event as the Events list shows it. The payload is as stored: each
// event's own fields (docs/api/events.md). `failed`: it records a failed
// check (a wrong password, a refused code, …).
export type AdminEvent = {
  id: string;
  name: string;
  createdAt: string;
  failed: boolean;
  actor: AdminRef | null;
  user: AdminRef | null;
  workspace: AdminRef | null;
  ip: string | null;
  payload: Record<string, unknown>;
};
// The Events page's time ranges, resolved by the API in UTC; "24h" is the
// default, and "all" lists rows with no charts.
export const eventWindows = [
  "1h",
  "24h",
  "today",
  "yesterday",
  "7d",
  "all",
] as const;
export type EventWindow = (typeof eventWindows)[number];
export const defaultEventWindow: EventWindow = "24h";
// The Events list's filters (docs/api/admin.md), as in its page URL and its
// API query: name tokens (catalogue names and group IDs such as "auth.*" or
// "admin-reads", written as one comma-separated `name`), failed checks
// only, an account, a workspace, an email, an IP or range, a relative
// window or RFC 3339 times (never both), and the cursor (rows older than
// that event).
export type EventFilters = {
  names?: string[];
  outcome?: "failed";
  account?: string;
  workspace?: string;
  email?: string;
  ip?: string;
  window?: EventWindow;
  from?: string;
  to?: string;
  before?: string;
};
// A cursor page: rows older than the cursor, and the next one's cursor.
export type CursorPage<T> = { data: T[]; next: string | null };
// The kinds of failed check, in the failures chart's order, and how the
// page names them (mirrors FailureKinds in internal/store).
export const failureKinds = [
  "sign_in",
  "second_factor",
  "password",
  "sign_up_code",
  "admin_door",
] as const;
export type FailureKind = (typeof failureKinds)[number];
export const failureLabels: Record<FailureKind, string> = {
  sign_in: "Sign-in",
  second_factor: "Second factor",
  password: "Password checks",
  sign_up_code: "Sign-up codes",
  admin_door: "Admin door",
};
// One column of a summary chart: its start, a count per series.
export type SummaryBucket = { start: string; counts: number[] };
// The charts' numbers, served with the newest page (summary=include), or
// why they weren't computed: a window over 7 days ("too_wide"), past their
// deadline ("too_slow"), or two summaries already running ("busy").
export type EventSummary =
  | {
      status: "ok";
      bucketSeconds: number;
      total: number;
      // Series are group IDs ("auth.*") or, inside one group, names; then
      // "other".
      stackedBy: "group" | "name";
      series: string[];
      buckets: SummaryBucket[];
      topNames: { name: string; count: number }[];
      // Null when no picked event can record a failed check.
      failures: {
        total: number;
        series: FailureKind[];
        buckets: SummaryBucket[];
      } | null;
      limitsReached: number;
      signInAttempts: number;
    }
  | { status: "too_wide" | "too_slow" | "busy" };
// An Events page: the rows and cursor, the window the API resolved (null
// for all time; `from` null for a range with no start), the account and
// workspace filters by name, and the summary when asked for.
export type AdminEventPage = CursorPage<AdminEvent> & {
  window: { from: string | null; to: string } | null;
  refs: { account: AdminRef | null; workspace: AdminRef | null };
  summary?: EventSummary;
};
// A group of event names, from the catalogue: "auth.*" holds sub-groups
// ("auth.mfa.*") and single names; "admin-reads" the admin reads.
export type EventGroup = {
  id: string;
  label: string;
  names: string[];
  groups: EventGroup[];
};
export const deliveryStatuses = [
  "failed",
  "retrying",
  "pending",
  "delivered",
] as const;
export type DeliveryStatus = (typeof deliveryStatuses)[number];
export type AdminDelivery = {
  listener: string;
  status: DeliveryStatus;
  attempts: number;
  nextAttemptAt: string | null;
  deliveredAt: string | null;
  failedAt: string | null;
  lastError: string;
};
export type AdminEventDetail = AdminEvent & {
  userAgent: string;
  deliveries: AdminDelivery[];
  related: AdminEvent[];
};
// A delivery in the Mail page's list; `to` is the event's email, if any.
export type AdminDeliveryRow = AdminDelivery & {
  event: { id: string; name: string; createdAt: string };
  to: string | null;
};
// The filters' names, from GET /api/admin/catalogue: every event name in
// catalogue order, and their groups.
export type AdminCatalogue = {
  events: string[];
  eventGroups: EventGroup[];
  limits: string[];
  listeners: string[];
};
export const mailWindows = ["24h", "7d"] as const;
export type MailWindow = (typeof mailWindows)[number];
// The Mail page's state in its URL: the summary's window, the deliveries'
// status tab and cursor.
export type DeliveryPageQuery = {
  window?: MailWindow;
  status?: DeliveryStatus;
  before?: string;
};
// Kinds of email, as the volume counts them, and of request, as "asked
// for vs sent" compares them.
export type MailKind =
  | "password_reset"
  | "invitation"
  | "registration_code"
  | "account_exists"
  | "email_verification";
export type RequestKind =
  "password_reset" | "registration" | "email_verification";
export type AdminMail = {
  window: MailWindow;
  driver: {
    driver: string;
    host?: string;
    port?: number;
    tls?: string;
    fromAddress?: string;
    fromName?: string;
  };
  queue: {
    listener: string;
    due: number;
    retrying: number;
    oldestPendingAt: string | null;
    failed: number;
    delivered: number;
  }[];
  volume: { hour: string; kind: MailKind; count: number }[];
  requests: { kind: RequestKind; requested: number; sent: number }[];
  topRecipients: {
    email: string;
    total: number;
    resets: number;
    invitations: number;
    registrations: number;
  }[];
  lastDeliveredAt: string | null;
  lastFailure: { listener: string; failedAt: string; error: string } | null;
};
// The Monitoring pages built so far (docs/api/monitoring.md), and their
// windows: what the charts and review lists cover. Signals keep their own.
export const monitoringPages = [
  "overview",
  "security",
  "limits",
  "ips",
  "accounts",
  "workspaces",
  "usage",
  "integrity",
  "performance",
  "errors",
  "configuration",
] as const;
export type MonitoringPage = (typeof monitoringPages)[number];
export const monitoringWindows = ["1h", "24h", "7d"] as const;
export type MonitoringWindow = (typeof monitoringWindows)[number];
export const defaultMonitoringWindow: MonitoringWindow = "24h";
// How a signal stands: fine, worth a look, a problem now.
export type Level = "ok" | "warn" | "critical";
// A signal as read: its value (in its unit), its level and thresholds (0 or
// missing: none), what it's counted over, and the Events filter behind it.
export type Signal = {
  name: string;
  label: string;
  unit: "count" | "percent" | "ms" | "minutes" | "per_minute";
  value: number;
  sample: number;
  level: Level;
  warn?: number;
  critical?: number;
  minSample?: number;
  windowSeconds: number;
  drill?: Record<string, string>;
  // It couldn't be read in time: its value and level mean nothing.
  unavailable?: boolean;
};
// A Monitoring page's answer: its window as resolved, its signals, its
// own sections, and the sections that couldn't be read in time (by their
// key in `sections`; their data is missing).
export type MonitoringView<S> = {
  page: MonitoringPage;
  window: {
    name: MonitoringWindow;
    from: string;
    to: string;
    bucketSeconds: number;
  };
  signals: Signal[];
  sections: S;
  unavailable: string[];
};
// A chart's numbers: a column per bucket, counts lining up with series.
export type MonitorSeries = { series: string[]; buckets: SummaryBucket[] };
// An account named in a review list: ID and name, never its email.
export type AccountRef = { id: string; name?: string };
export type AddressCount = { ip: string; count: number };
export type SharedLimit = {
  name: string;
  max: number;
  windowSeconds: number;
  hits: number;
  resetsAt: string | null;
  reached: number;
  lastReached: string | null;
};
// The Overview's tiles: their signals (by name), their page, a sparkline.
export type MonitoringTile = {
  id: string;
  label: string;
  page: string;
  signals: string[];
  series: {
    label: string;
    unit: Signal["unit"];
    points: { start: string; value: number }[];
  } | null;
};
// How the live servers store metrics (METRICS_STORE): with it off the
// request, latency and pool numbers read nothing. Empty: no server live.
export type MetricsStore = "on" | "off" | "mixed" | "";
export type OverviewSections = {
  tiles: MonitoringTile[];
  metricsStore: MetricsStore;
};
export type FailedChecksRow = {
  period: string;
  account: AccountRef;
  failures: number;
};
// A request that sends mail or makes an account: asked for and done per
// bucket, the addresses asking most, and the shared limit on it.
export type MonitoringFlow = {
  series: MonitorSeries;
  topAddresses: AddressCount[];
  limit: SharedLimit | null;
};
export type DoorOutcome =
  | "wrong_password"
  | "unknown_account"
  | "not_admin"
  | "mfa_off"
  | "access_refused"
  | "session_moved";
export type SecuritySections = {
  signIns: MonitorSeries;
  stuffing: {
    period: string;
    ip: string;
    emails: number;
    attempts: number;
    successes: number;
  }[];
  guessing: {
    period: string;
    email: string;
    addresses: number;
    failures: number;
  }[];
  unknown: { attempts: number; unknown: number };
  successAfterFailures: {
    period: string;
    account: AccountRef;
    failures: number;
    signedInAt: string;
  }[];
  takeovers: {
    changedAt: string;
    change: string;
    event: string;
    account: AccountRef;
    signedInAt: string;
    network: string;
  }[];
  secondFactor: {
    failures: FailedChecksRow[];
    backupCodes: { used: number; lowAccounts: number };
  };
  reauthentication: FailedChecksRow[];
  resets: MonitoringFlow;
  signUps: MonitoringFlow;
  adminDoor: {
    counts: { outcome: DoorOutcome; count: number }[];
    series: MonitorSeries;
    recent: {
      at: string;
      outcome: DoorOutcome;
      ip: string | null;
      event: string;
    }[];
  };
  sessions: { series: MonitorSeries; live: number; accountsAtCap: number };
};
export type LimitsSections = {
  reached: MonitorSeries;
  byLimit: { limit: string; windows: number; lastReached: string }[];
  refused: { label: string; count: number }[];
  shared: SharedLimit[];
  blocked: { name: string; max: number; subjects: number | null }[];
  topAccounts: { account: AccountRef; windows: number }[];
  topAddresses: AddressCount[];
  budgets: { account: AccountRef; limit: string; windows: number }[];
  quotas: { label: string; count: number }[];
  routes: {
    route: string;
    refused: number;
    requests: number;
    serverErrors: number;
  }[];
};
// The IP addresses page: the top addresses both ways (`network`: an IPv6
// /64, as the signals count; otherwise one address: listed by /64, the
// /64s and the IPv4 addresses; each on its own, every address), and
// sign-up clusters.
export type AddressRow = {
  ip: string;
  network: boolean;
  events: number;
  failedSignIns: number;
  signUps: number;
  limitsReached: number;
  accounts: number;
};
export type IpsSections = {
  addresses: AddressRow[];
  clusters: { period: string; ip: string; accounts: number }[];
  // How many IP blocks are live (the IP blocks page lists them).
  blocks: { live: number };
};
// One address's page: what an address (or an IPv6 /64) did in a window.
export type AddressView = {
  ip: string;
  // An IPv6 address's /64, to open.
  network?: string;
  window: MonitoringView<unknown>["window"];
  events: number;
  firstSeen: string | null;
  lastSeen: string | null;
  byName: { name: string; count: number }[];
  accounts: {
    account: AccountRef;
    signedIn: number;
    registered: number;
    failed: number;
  }[];
  unknownEmails: number;
  limits: { limit: string; windows: number; lastReached: string }[];
  addresses: AddressCount[];
  // The live IP blocks holding it, or part of its /64.
  blocks: { network: string; expiresAt: string }[];
};
// The Accounts page's review list: each account with the signals it
// tripped in its busiest day.
export type AccountReviewSignal =
  | "workspaces"
  | "invitations"
  | "acceptance"
  | "projects"
  | "links"
  | "limits"
  | "checks";
export type WorkspaceRef = { id: string; name?: string };
export type AccountReview = {
  account: AccountRef;
  createdAt: string;
  new: boolean;
  signals: {
    signal: AccountReviewSignal;
    count: number;
    of?: number;
    workspace?: WorkspaceRef;
  }[];
};
export type AccountCount = { account: AccountRef; count: number };
export type AccountsSections = {
  review: AccountReview[];
  topWriters: AccountCount[];
  topOwners: AccountCount[];
};
// The Workspaces page.
export type WorkspacesSections = {
  quotas: {
    caps: {
      projects: number;
      members: number;
      invitations: number;
      owned: number;
    };
    workspaces: {
      workspace: WorkspaceRef;
      projects: number;
      members: number;
      invitations: number;
      used: number;
    }[];
    owners: { account: AccountRef; owned: number; used: number }[];
  };
  rows: {
    // Each table is counted up to one more than cap: more reads "over".
    cap: number;
    tables: { table: string; label: string }[];
    workspaces: { workspace: WorkspaceRef; counts: number[]; total: number }[];
  };
  growth: { workspace: WorkspaceRef; created: number; events: number }[];
  invitations: {
    workspace: WorkspaceRef;
    sent: number;
    accepted: number;
    revoked: number;
    expired: number;
  }[];
  throwaways: {
    account: AccountRef;
    workspaces: number;
    invitations: number;
    lastDeleted: string;
  }[];
  totals: { estimate: number; series: MonitorSeries };
};
// The Usage page.
export type Activation = {
  accounts: number;
  activated: number;
  from: string;
  to: string;
};
export type SizeCount = { size: string; workspaces: number };
export type UsageSections = {
  active: { day: number; week: number; month: number; withMfa: number };
  signUps: MonitorSeries;
  activation: { workspace: Activation; invited: Activation };
  features: MonitorSeries;
  sizes: { members: SizeCount[]; projects: SizeCount[] };
  inactive: {
    before: string;
    count: number;
    accounts: {
      account: AccountRef;
      lastSeenAt: string | null;
      createdAt: string;
      admin: boolean;
    }[];
  };
};
// The Integrity page: each check's rows (its count and level are its
// signal's), then duplicates and retries over the window.
export type CheckResult = {
  count: number;
  capped?: boolean;
  samples: string[];
};
export type IntegritySections = {
  checks: { name: string; capped?: boolean; samples: string[] }[];
  duplicates: CheckResult & { byName: { name: string; count: number }[] };
  duplicateSends: CheckResult;
  retriedDeliveries: CheckResult;
  timedOutWrites: { route: string; count: number }[];
};
// The Performance page: percentiles in milliseconds.
export type Latencies = { p50: number; p95: number; p99: number };
export type LatencyRow = Latencies & { label: string; count: number };
export type GaugePoint = { start: string; value: number };
export type RouteRow = Latencies & {
  route: string;
  requests: number;
  status4xx: number;
  status429: number;
  status5xx: number;
  timeouts: number;
};
export type ServerRow = {
  id: string;
  hostname: string;
  version: string;
  startedAt: string;
  lastSeenAt: string;
  live: boolean;
  goroutines: number | null;
  heapBytes: number | null;
  memoryBytes: number | null;
  gcPauseMs: number | null;
  poolOpen: number | null;
  poolInUse: number | null;
  poolIdle: number | null;
  measuredAt: string | null;
};
// pg_stat_statements' statements, as PostgreSQL normalised them ($1 for
// every constant); available false when the database doesn't have it.
export type StatementStats = {
  available: boolean;
  statements: {
    query: string;
    calls: number;
    totalMs: number;
    meanMs: number;
    rows: number;
  }[];
  hidden: number;
};
export type PerformanceSections = {
  metricsStore: MetricsStore;
  requests: (Latencies & {
    start: string;
    requests: number;
    serverErrors: number;
  })[];
  routes: RouteRow[];
  database: {
    operations: LatencyRow[];
    slowQueries: { label: string; count: number }[];
    inUse: GaugePoint[];
    waits: { from: string; waits: number; waitedMs: number; minutes: number };
    statements: StatementStats | null;
  };
  worker: {
    deliveries: MonitorSeries;
    lag: LatencyRow[];
    pending: GaugePoint[];
  };
  slotsBusy: number;
  servers: ServerRow[];
};
// The Errors page: grouped errors (app_errors). A browser's group
// (source client) holds a user's text: shown as text, never a link.
export type ErrorSource =
  "request" | "panic" | "worker" | "background" | "client";
export type ErrorGroup = {
  id: string;
  source: ErrorSource;
  route: string;
  message: string;
  count: number;
  firstSeenAt: string;
  lastSeenAt: string;
  lastRequestId: string;
  // Only in a release's shape (a revision, or dev); empty otherwise.
  lastVersion: string;
  resolvedAt: string | null;
};
export type Release = { version: string; startedAt: string };
export type ErrorsSections = {
  release: Release | null;
  groups: ErrorGroup[];
  occurrences: MonitorSeries;
};
// One group's page.
export type ErrorView = {
  window: MonitoringView<unknown>["window"];
  group: ErrorGroup & { stack: string };
  release: Release | null;
  occurrences: MonitorSeries;
};
// The Configuration page: servers, their settings side by side (the
// compared servers' values in their order), and warnings by ID.
export type ConfigurationSections = {
  servers: {
    id: string;
    hostname: string;
    version: string;
    startedAt: string;
    lastSeenAt: string;
    live: boolean;
  }[];
  compared: string[];
  settings: { name: string; values: string[]; differs: boolean }[];
  warnings: { id: string; count?: number }[];
};

// An alert as the Alerts page shows it: a signal that stayed at its level.
// Details hold its Events drill-down and an integrity check's sample IDs
// (the dashboard only: emails and the webhook carry neither).
export type AdminAlertPerson = { id: string; name: string; email: string };
export type AdminAlert = {
  id: string;
  signal: string;
  title: string;
  unit: Signal["unit"];
  page: string;
  meaning: string;
  level: "warn" | "critical";
  openedAt: string;
  resolvedAt: string | null;
  lastValue: number;
  peakValue: number;
  warn?: number;
  critical?: number;
  forSeconds: number;
  hourly: boolean;
  details: { drill?: Record<string, string>; samples?: string[] };
  notified: "mail" | "held" | "digest" | "quiet";
  lastNotifiedAt: string | null;
  acknowledgedAt: string | null;
  acknowledgedBy: AdminAlertPerson | null;
  silencedUntil: string | null;
  silencedBy: AdminAlertPerson | null;
};
export const alertStates = ["open", "resolved", "all"] as const;
export type AlertState = (typeof alertStates)[number];
export type AdminAlertPage = { data: AdminAlert[]; next: string | null };
export const alertSilences = [
  { value: "1h", label: "1 hour" },
  { value: "4h", label: "4 hours" },
  { value: "24h", label: "24 hours" },
] as const;
export type AlertSilence = (typeof alertSilences)[number]["value"];
