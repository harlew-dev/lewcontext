import { PlusIcon } from "@heroicons/react/16/solid";
import { appLayout } from "@/components/layout/application-layout";
import { ServiceUnavailable } from "@/components/layout/status-page";
import { ProjectDialog } from "@/components/app/projects/project-dialog";
import { ProjectNotices } from "@/components/app/projects/project-notices";
import { ProjectTable } from "@/components/app/projects/project-table";
import { RefreshButton } from "@/components/app/projects/refresh-button";
import { useProjects } from "@/components/app/projects/use-projects";
import { Badge, type BadgeColor } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Divider } from "@/components/ui/divider";
import { ListSkeleton } from "@/components/ui/feedback";
import { Heading, Subheading } from "@/components/ui/heading";
import { routes } from "@/lib/routes";
import type { Project, WorkspacePageProps, WorkspaceProps } from "@/lib/types";
export { workspaceProps as getServerSideProps } from "@/lib/server";
export default function Dashboard(props: WorkspacePageProps) {
  if ("unavailable" in props) return <ServiceUnavailable />;
  return <DashboardContent {...props} />;
}
Dashboard.getLayout = appLayout;
function DashboardContent(props: WorkspaceProps) {
  const state = useProjects(props);
  // Summaries come from the loaded projects, so wait for data before showing them.
  const ready = !state.loading && !state.error;
  const recent = [...state.projects]
    .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
    .slice(0, 5);
  return (
    <>
      <Heading>Welcome back, {props.session.user.name.split(" ")[0]}.</Heading>
      <div className="mt-8 flex items-end justify-between">
        <Subheading>Overview</Subheading>
        <Button onClick={() => state.setDialog("new")}>
          <PlusIcon />
          New project
        </Button>
      </div>
      <ProjectNotices state={state} />
      {ready && <Stats projects={state.projects} />}
      <div className="mt-14 flex items-end justify-between">
        <Subheading>Recent projects</Subheading>
        <div className="flex items-center gap-2">
          <RefreshButton state={state} />
          <Button plain href={routes.projects}>
            View all
          </Button>
        </div>
      </div>
      {state.loading ? (
        <ListSkeleton label="Loading projects" />
      ) : !state.error || state.projects.length > 0 ? (
        <ProjectTable
          className="mt-4 [--gutter:--spacing(6)] lg:[--gutter:--spacing(10)]"
          projects={recent}
          onEdit={state.setDialog}
          onCreate={() => state.setDialog("new")}
        />
      ) : null}
      <ProjectDialog state={state} csrfToken={props.session.csrfToken} />
    </>
  );
}
function Stats({ projects }: { projects: Project[] }) {
  const count = (status: Project["status"]) =>
    projects.filter((project) => project.status === status).length;
  return (
    <div className="mt-4 grid gap-8 sm:grid-cols-2 xl:grid-cols-4">
      <Stat title="Total projects" value={projects.length} />
      <Stat
        title="In progress"
        value={count("In progress")}
        total={projects.length}
        color="purple"
      />
      <Stat
        title="Completed"
        value={count("Completed")}
        total={projects.length}
        color="lime"
      />
      <Stat
        title="Planned"
        value={count("Planned")}
        total={projects.length}
        color="zinc"
      />
    </div>
  );
}
function Stat({
  title,
  value,
  total,
  color = "zinc",
}: {
  title: string;
  value: number;
  total?: number;
  color?: BadgeColor;
}) {
  const share = total ? Math.round((value / total) * 100) : 0;
  return (
    <div>
      <Divider />
      <div className="mt-6 text-lg/6 font-medium sm:text-sm/6">{title}</div>
      <div className="mt-3 text-3xl/8 font-semibold sm:text-2xl/8">{value}</div>
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
