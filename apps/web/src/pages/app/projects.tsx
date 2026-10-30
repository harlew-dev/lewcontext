import { useState } from "react";
import { MagnifyingGlassIcon, PlusIcon } from "@heroicons/react/16/solid";
import { appLayout } from "@/components/layout/application-layout";
import { ServiceUnavailable } from "@/components/layout/status-page";
import { ProjectDialog } from "@/components/app/projects/project-dialog";
import { ProjectNotices } from "@/components/app/projects/project-notices";
import { ProjectTable } from "@/components/app/projects/project-table";
import { RefreshButton } from "@/components/app/projects/refresh-button";
import { useProjects } from "@/components/app/projects/use-projects";
import { Button } from "@/components/ui/button";
import { ListSkeleton } from "@/components/ui/feedback";
import { Heading } from "@/components/ui/heading";
import { Input, InputGroup } from "@/components/ui/input";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { Text } from "@/components/ui/text";
import {
  statuses,
  type Project,
  type WorkspacePageProps,
  type WorkspaceProps,
} from "@/lib/types";
export { workspaceProps as getServerSideProps } from "@/lib/server";
type StatusFilter = "all" | Project["status"];
export default function Projects(props: WorkspacePageProps) {
  if ("unavailable" in props) return <ServiceUnavailable />;
  return <ProjectsContent {...props} />;
}
Projects.getLayout = appLayout;
function ProjectsContent(props: WorkspaceProps) {
  const state = useProjects(props);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");
  const query = search.trim().toLowerCase();
  const visible = state.projects.filter(
    (project) =>
      (status === "all" || project.status === status) &&
      `${project.name} ${project.description}`.toLowerCase().includes(query),
  );
  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="max-sm:w-full sm:flex-1">
          <Heading>Projects</Heading>
          <Text className="mt-1">
            {state.projects.length} projects in your workspace
          </Text>
          <div className="mt-4 flex max-w-xl gap-4">
            <div className="flex-1">
              <InputGroup>
                <MagnifyingGlassIcon />
                <Input
                  aria-label="Search projects"
                  placeholder="Search projects…"
                  value={search}
                  onChange={(e) => setSearch(e.currentTarget.value)}
                />
              </InputGroup>
            </div>
            <div className="w-44">
              <Listbox<StatusFilter>
                aria-label="Filter by status"
                value={status}
                onChange={setStatus}
              >
                <ListboxOption value="all">
                  <ListboxLabel>All statuses</ListboxLabel>
                </ListboxOption>
                {statuses.map((item) => (
                  <ListboxOption key={item} value={item}>
                    <ListboxLabel>{item}</ListboxLabel>
                  </ListboxOption>
                ))}
              </Listbox>
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <RefreshButton state={state} />
          <Button onClick={() => state.setDialog("new")}>
            <PlusIcon />
            New project
          </Button>
        </div>
      </div>
      <ProjectNotices state={state} />
      <div className="mt-10">
        {state.loading ? (
          <ListSkeleton label="Loading projects" />
        ) : !state.error || state.projects.length > 0 ? (
          <ProjectTable
            projects={visible}
            onEdit={state.setDialog}
            onCreate={() => state.setDialog("new")}
            filtered={Boolean(query) || status !== "all"}
          />
        ) : null}
      </div>
      <ProjectDialog state={state} csrfToken={props.session.csrfToken} />
    </>
  );
}
