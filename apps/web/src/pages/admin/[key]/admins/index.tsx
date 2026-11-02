import { useState } from "react";
import {
  ArrowDownTrayIcon,
  EllipsisHorizontalIcon,
} from "@heroicons/react/16/solid";
import { AdminActionDialog } from "@/components/app/admin/admin-action-dialog";
import { MfaBadge } from "@/components/app/admin/admin-list";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dropdown,
  DropdownButton,
  DropdownItem,
  DropdownLabel,
  DropdownMenu,
} from "@/components/ui/dropdown";
import { EmptyState, ListSkeleton, Notice } from "@/components/ui/feedback";
import { Heading, Subheading } from "@/components/ui/heading";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Code, Text } from "@/components/ui/text";
import { toast } from "@/components/ui/toast";
import { errorMessage } from "@/lib/api";
import { adminPaths, useAdminApi } from "@/lib/api/admin";
import { adminCan, useAdminSession } from "@/lib/admin";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAction } from "@/lib/use-action";
import { useAdminResource } from "@/lib/use-admin-resource";
import { useOpenCount } from "@/lib/use-open-count";
import { Field, Label } from "@/components/ui/fieldset";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import {
  alertEmails,
  type AdminAccount,
  type AdminAccountSession,
  type AdminRole,
  type AlertEmail,
} from "@/types/admin";
// Who holds admin, for an access review: each admin's role, MFA, when they
// were granted admin and last signed in to the dashboard, and every live
// admin session. Opening it records one read (admin.admin.listed). Roles
// change only from the command line. Any admin may sign their own other
// admin sessions out; an operator may sign another admin out (a lost
// laptop, a session that moved), behind a fresh code and a reason: the one
// action on another admin.
export default function AdminAdminIndex() {
  const list = useAdminResource(adminPaths.admins.index());
  const admins = list.data?.data;
  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <Heading>Admins</Heading>
        <Button
          outline
          disabled={!admins}
          disabledReason="Loading admins"
          onClick={() => admins && downloadAdmins(admins)}
        >
          <ArrowDownTrayIcon />
          Download CSV
        </Button>
      </div>
      <Text className="mt-2">
        Admin and its role are granted from the command line only:{" "}
        <Code>admin grant -email … -role viewer|operator</Code>.
      </Text>
      {list.error && (
        <Notice variant="error" className="mt-6" onRetry={list.refresh}>
          {list.error}
        </Notice>
      )}
      {admins ? (
        <>
          <AdminTable admins={admins} onChanged={list.refresh} />
          <AlertEmailSetting admins={admins} onDone={list.refresh} />
          <Subheading className="mt-12">Live sessions</Subheading>
          <SessionTable admins={admins} />
          <OtherSessions admins={admins} onDone={list.refresh} />
        </>
      ) : (
        list.loading && (
          <div className="mt-10">
            <ListSkeleton label="Loading admins" />
          </div>
        )
      )}
    </>
  );
}
const roleBadges: Record<AdminRole, { label: string; color: "zinc" | "pink" }> =
  {
    viewer: { label: "Viewer", color: "zinc" },
    operator: { label: "Operator", color: "pink" },
  };
// How long without an admin sign-in before the page suggests revoking.
const staleAfter = 90 * 24 * 60 * 60 * 1000;
// Whether the admin hasn't signed in to the dashboard for 90 days (or, never
// signed in, was granted that long ago). Events are pruned after 90 days by
// default, so an older sign-in reads as none.
function stale(admin: AdminAccount, now: number): boolean {
  const last = admin.lastSignedInAt ?? admin.grantedAt;
  return now - new Date(last).getTime() > staleAfter;
}
// Whether the admin may still set up MFA at the door (admin grant opened
// the window and it hasn't closed).
function setupOpen(admin: AdminAccount, now: number): boolean {
  return (
    !admin.mfaEnabled &&
    admin.mfaSetupUntil !== null &&
    new Date(admin.mfaSetupUntil).getTime() > now
  );
}
function AdminTable({
  admins,
  onChanged,
}: {
  admins: AdminAccount[];
  onChanged: () => void;
}) {
  // When the page opened: what "90 days" and "open" are measured from.
  const [now] = useState(Date.now);
  const me = useAdminSession();
  const operator = adminCan(me.role, "operator");
  const [signingOut, setSigningOut] = useState<AdminAccount | null>(null);
  if (admins.length === 0)
    return (
      <div className="mt-10">
        <EmptyState
          title="No admins"
          description="Grant one from the command line: admin grant -email … -confirm."
        />
      </div>
    );
  return (
    <>
      <Table className="mt-10">
        <TableHead>
          <TableRow>
            <TableHeader>Admin</TableHeader>
            <TableHeader className="max-sm:hidden">Granted</TableHeader>
            <TableHeader>Last sign-in</TableHeader>
            <TableHeader className="max-md:hidden">Sessions</TableHeader>
            {operator && (
              <TableHeader className="w-0">
                <span className="sr-only">Actions</span>
              </TableHeader>
            )}
          </TableRow>
        </TableHead>
        <TableBody>
          {admins.map((admin) => (
            <TableRow key={admin.user.id}>
              <TableCell>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{admin.user.name}</span>
                  <Badge color={roleBadges[admin.role].color}>
                    {roleBadges[admin.role].label}
                  </Badge>
                  <MfaBadge enabled={admin.mfaEnabled} />
                  {setupOpen(admin, now) && <Badge>Setup window open</Badge>}
                </div>
                <div className="text-zinc-500">{admin.user.email}</div>
              </TableCell>
              <TableCell className="text-zinc-500 max-sm:hidden">
                {formatDate(admin.grantedAt, "long")}
              </TableCell>
              <TableCell>
                <LastSignIn admin={admin} stale={stale(admin, now)} />
              </TableCell>
              <TableCell className="max-md:hidden">
                {admin.sessions.length}
              </TableCell>
              {operator && (
                <TableCell className="w-0 text-right">
                  {admin.user.id !== me.user.id && (
                    <Dropdown>
                      <DropdownButton
                        plain
                        aria-label={`Actions for ${admin.user.name}`}
                      >
                        <EllipsisHorizontalIcon />
                      </DropdownButton>
                      <DropdownMenu anchor="bottom end">
                        <DropdownItem
                          variant="danger"
                          onClick={() => setSigningOut(admin)}
                        >
                          <DropdownLabel>Sign out</DropdownLabel>
                        </DropdownItem>
                      </DropdownMenu>
                    </Dropdown>
                  )}
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <SignOutDialog
        admin={signingOut}
        onClose={() => setSigningOut(null)}
        onDone={onChanged}
      />
    </>
  );
}
// Signing another admin out of every admin and app session: they keep
// admin and can sign in again with their password and code. For anything
// more (revoking admin, resetting their MFA), the command line.
function SignOutDialog({
  admin,
  onClose,
  onDone,
}: {
  admin: AdminAccount | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const api = useAdminApi();
  const opened = useOpenCount(admin !== null);
  // The last one stays rendered while the dialog animates out.
  const [shown, setShown] = useState<AdminAccount | null>(null);
  if (admin && admin !== shown) setShown(admin);
  if (!shown) return null;
  return (
    <AdminActionDialog
      key={opened}
      open={admin !== null}
      onClose={onClose}
      title={`Sign ${shown.user.name} out?`}
      description="Every admin session and app session of theirs ends at once. They stay an admin and can sign in again with their password and a code; to take admin away, use the command line (admin revoke)."
      confirm="Sign out"
      variant="danger"
      action={(reason) => api.adminSessions(shown.user.id).destroy(reason)}
      onDone={() => {
        toast.success(`${shown.user.name} is signed out.`);
        onDone();
      }}
    />
  );
}
function LastSignIn({ admin, stale }: { admin: AdminAccount; stale: boolean }) {
  const when = admin.lastSignedInAt
    ? formatDate(admin.lastSignedInAt, "long")
    : "Never";
  if (!stale) return <span className="text-zinc-500">{when}</span>;
  return (
    <span className="text-amber-700 dark:text-amber-400">
      {when} · no admin sign-in for 90 days: consider revoking
    </span>
  );
}
// Every live admin session, each admin's newest first.
function SessionTable({ admins }: { admins: AdminAccount[] }) {
  const rows = admins.flatMap((admin) =>
    admin.sessions.map((session) => ({ admin, session })),
  );
  if (rows.length === 0)
    return <Text className="mt-4">No admin is signed in.</Text>;
  return (
    <Table className="mt-4">
      <TableHead>
        <TableRow>
          <TableHeader>Admin</TableHeader>
          <TableHeader>Signed in</TableHeader>
          <TableHeader className="max-sm:hidden">Last seen</TableHeader>
          <TableHeader className="max-md:hidden">From</TableHeader>
        </TableRow>
      </TableHead>
      <TableBody>
        {rows.map(({ admin, session }) => (
          <SessionRow
            key={`${admin.user.id} ${session.createdAt}`}
            admin={admin}
            session={session}
          />
        ))}
      </TableBody>
    </Table>
  );
}
function SessionRow({
  admin,
  session,
}: {
  admin: AdminAccount;
  session: AdminAccountSession;
}) {
  return (
    <TableRow>
      <TableCell>
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{admin.user.name}</span>
          {session.current && <Badge color="lime">This session</Badge>}
          {session.previousKey && <Badge>Retiring address</Badge>}
        </div>
      </TableCell>
      <TableCell className="text-zinc-500">
        {formatDateTime(session.createdAt)}
      </TableCell>
      <TableCell className="text-zinc-500 max-sm:hidden">
        {formatDateTime(session.lastSeenAt)}
      </TableCell>
      <TableCell className="max-w-xs text-zinc-500 max-md:hidden">
        <div className="font-mono">{session.ip}</div>
        <div className="truncate" title={session.userAgent}>
          {session.userAgent || "Unknown browser"}
        </div>
      </TableCell>
    </TableRow>
  );
}
// Signing out your own other admin sessions: no code and no role needed,
// since it only takes your access away. Signing out another admin is an
// action for a later plan.
function OtherSessions({
  admins,
  onDone,
}: {
  admins: AdminAccount[];
  onDone: () => void;
}) {
  const api = useAdminApi();
  const me = useAdminSession().user.id;
  const mine = admins.find((admin) => admin.user.id === me);
  const others = (mine?.sessions ?? []).filter((s) => !s.current).length;
  const revoke = useAction(() => api.sessions().destroyOthers(), {
    onError: (e) => {
      toast.error(errorMessage(e));
      return true;
    },
  });
  async function revokeOthers() {
    if (!(await revoke.run()).ok) return;
    toast.success("Your other admin sessions have been signed out.");
    onDone();
  }
  return (
    <div className="mt-4 flex justify-end">
      <Button
        outline
        loading={revoke.loading}
        disabled={others === 0}
        disabledReason="You have no other admin sessions"
        onClick={revokeOthers}
      >
        Sign out my other sessions
      </Button>
    </div>
  );
}
// The access review an auditor asks for, built from the list already
// loaded, so it records nothing more. Cells are quoted, and any that a
// spreadsheet would read as a formula start with an apostrophe.
function adminsCsv(admins: AdminAccount[]): string {
  const rows = [
    ["Email", "Role", "MFA", "Granted", "Last sign-in"],
    ...admins.map((admin) => [
      admin.user.email,
      admin.role,
      admin.mfaEnabled ? "on" : "off",
      admin.grantedAt,
      admin.lastSignedInAt ?? "",
    ]),
  ];
  return rows.map((row) => row.map(csvCell).join(",")).join("\r\n") + "\r\n";
}
function csvCell(value: string): string {
  const safe = /^[=+\-@\t\r]/.test(value) ? `'${value}` : value;
  return `"${safe.replaceAll('"', '""')}"`;
}
function downloadAdmins(admins: AdminAccount[]) {
  const blob = new Blob([adminsCsv(admins)], {
    type: "text/csv;charset=utf-8",
  });
  const href = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = href;
  link.download = `admins-${new Date().toISOString().slice(0, 10)}.csv`;
  link.click();
  URL.revokeObjectURL(href);
}

const alertEmailLabels: Record<AlertEmail, string> = {
  all: "Every alert (warnings too)",
  critical: "Critical alerts only",
  none: "None",
};
// Which alerts you're emailed (docs/api/monitoring.md#alerts): your own
// setting only, so it needs no role or code; it changes only your inbox.
// Critical only by default.
function AlertEmailSetting({
  admins,
  onDone,
}: {
  admins: AdminAccount[];
  onDone: () => void;
}) {
  const api = useAdminApi();
  const me = useAdminSession().user.id;
  const mine = admins.find((admin) => admin.user.id === me);
  const save = useAction(
    (setting: AlertEmail) => api.settings().update(setting),
    {
      onError: (e) => {
        toast.error(errorMessage(e));
        return true;
      },
    },
  );
  if (!mine) return null;
  async function change(setting: AlertEmail) {
    if (!(await save.run(setting)).ok) return;
    toast.success("Alert emails updated.");
    onDone();
  }
  return (
    <>
      <Subheading className="mt-12">Alert emails</Subheading>
      <Text className="mt-2 max-w-2xl">
        Alerts are emailed to admins who want them, with the signal and its
        numbers only: never a link into the dashboard or anyone&rsquo;s details.
        This is your setting; each admin chooses their own.
      </Text>
      <Field className="mt-4 max-w-sm">
        <Label>Email me</Label>
        <Listbox
          value={mine.alertEmail}
          disabled={save.busy}
          onChange={(value) => void change(value as AlertEmail)}
        >
          {alertEmails.map((setting) => (
            <ListboxOption key={setting} value={setting}>
              <ListboxLabel>{alertEmailLabels[setting]}</ListboxLabel>
            </ListboxOption>
          ))}
        </Listbox>
      </Field>
    </>
  );
}
