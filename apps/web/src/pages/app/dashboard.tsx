import type { GetServerSidePropsContext } from "next";
import { PlusIcon } from "@heroicons/react/16/solid";
import { appLayout } from "@/components/layout/application-layout";
import { ServiceUnavailable } from "@/components/layout/status-page";
import { ProjectDialog } from "@/components/app/projects/project-dialog";
import { ProjectNotices } from "@/components/app/projects/project-notices";
import {
  ProjectEmptyState,
  ProjectRow,
  ProjectTableHead,
} from "@/components/app/projects/project-table";
import { statusColors } from "@/components/app/projects/project-display";
import { RefreshButton } from "@/components/app/projects/refresh-button";
import { useProjectEditor } from "@/components/app/projects/use-project-editor";
import { Badge, type BadgeColor } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Divider } from "@/components/ui/divider";
import { ListSkeleton } from "@/components/ui/feedback";
import { Heading, Subheading } from "@/components/ui/heading";
import { Table, TableBody, TableHead } from "@/components/ui/table";
import { url, type Page } from "@/lib/pagination";
import { apiRoutes, routes } from "@/lib/routes";
import { appProps, initial } from "@/lib/server";
import type { AppPageProps, Initial, SessionProps } from "@/lib/types";
import { useApi } from "@/lib/use-api";
import { statuses, type Project } from "@/types/projects";
type Status = Project["status"];
type DashboardProps = SessionProps & {
  recent: Initial<Page<Project>>;
  counts: Initial<Page<Project>>[];
};
const failure = "We couldn’t load your projects. Please try again.";
// The five most recently updated projects; the page's item count is the total.
const recentPath = url(apiRoutes.projects, { perPage: 5 });
// A one-row page per status, read for its item count.
function countPath(status: Status) {
  return url(apiRoutes.projects, { status, perPage: 1 });
}
export function getServerSideProps(context: GetServerSidePropsContext) {
  return appProps(context, async (get) => {
    const [recent, ...counts] = await Promise.all([
      initial<Page<Project>>(get, recentPath, failure),
      ...statuses.map((status) =>
        initial<Page<Project>>(get, countPath(status), failure),
      ),
    ]);
    return { recent, counts };
  });
}
export default function Dashboard(props: AppPageProps<DashboardProps>) {
  if ("unavailable" in props) return <ServiceUnavailable />;
  return <DashboardContent {...props} />;
}
Dashboard.getLayout = appLayout;
function DashboardContent({ session, recent, counts }: DashboardProps) {
  const list = useApi(recentPath, session.user.id, recent);
  const editor = useProjectEditor();
  const total = list.data?.pages.items;
  return (
    <>
      <Heading>Welcome back, {session.user.name.split(" ")[0]}.</Heading>
      <div className="mt-8 flex items-end justify-between">
        <Subheading>Overview</Subheading>
        <Button onClick={() => editor.setDialog("new")}>
          <PlusIcon />
          New project
        </Button>
      </div>
      <ProjectNotices
        editor={editor}
        error={list.error}
        onRetry={list.refresh}
      />
      {total !== undefined && (
        <div className="mt-4 grid gap-8 sm:grid-cols-2 xl:grid-cols-4">
          <Stat title="Total projects" value={total} />
          {statuses.map((status) => (
            <StatusStat
              key={status}
              status={status}
              total={total}
              userId={session.user.id}
              initial={counts.find((c) => c.path === countPath(status))}
            />
          ))}
        </div>
      )}
      <div className="mt-14 flex items-end justify-between">
        <Subheading>Recent projects</Subheading>
        <div className="flex items-center gap-2">
          <RefreshButton
            refreshing={list.refreshing}
            onRefresh={list.refresh}
          />
          <Button plain href={routes.projects}>
            View all
          </Button>
        </div>
      </div>
      <RecentProjects
        page={list.data}
        loading={list.loading}
        onCreate={() => editor.setDialog("new")}
      />
      <ProjectDialog editor={editor} csrfToken={session.csrfToken} />
    </>
  );
}
function RecentProjects({
  page,
  loading,
  onCreate,
}: {
  page: Page<Project> | undefined;
  loading: boolean;
  onCreate: () => void;
}) {
  if (!page) return loading ? <ListSkeleton label="Loading projects" /> : null;
  if (!page.data.length) return <ProjectEmptyState onCreate={onCreate} />;
  return (
    <Table className="mt-4 [--gutter:--spacing(6)] lg:[--gutter:--spacing(10)]">
      <TableHead>
        <ProjectTableHead />
      </TableHead>
      <TableBody>
        {page.data.map((project) => (
          <ProjectRow key={project.id} project={project} />
        ))}
      </TableBody>
    </Table>
  );
}
function StatusStat({
  status,
  total,
  userId,
  initial,
}: {
  status: Status;
  total: number;
  userId: number;
  initial?: Initial<Page<Project>>;
}) {
  const count = useApi(countPath(status), userId, initial);
  return (
    <Stat
      title={status}
      value={count.data?.pages.items}
      total={total}
      color={statusColors[status]}
    />
  );
}
function Stat({
  title,
  value,
  total,
  color = "zinc",
}: {
  title: string;
  value: number | undefined;
  total?: number;
  color?: BadgeColor;
}) {
  const share = total && value ? Math.round((value / total) * 100) : 0;
  return (
    <div>
      <Divider />
      <div className="mt-6 text-lg/6 font-medium sm:text-sm/6">{title}</div>
      <div className="mt-3 text-3xl/8 font-semibold sm:text-2xl/8">
        {value ?? "–"}
      </div>
      <div className="mt-3 text-sm/6 sm:text-xs/6">
        {total === undefined ? (
          <span className="text-zinc-500">In your workspace</span>
        ) : (
          <>
            <Badge color={color}>{share}%</Badge>{" "}
            <span className="text-zinc-500">of all projects</span>
          </>
        )}
      </div>
    </div>
  );
}
