import { useNavigate, useSearchParams } from "react-router";
import { PlusIcon } from "@heroicons/react/16/solid";
import { ProjectDialog } from "@/components/app/projects/project-dialog";
import { ProjectNotices } from "@/components/app/projects/project-notices";
import {
  projectQuery,
  indexQuery,
  projectsPath,
} from "@/components/app/projects/project-query";
import {
  ProjectEmptyState,
  ProjectRow,
  ProjectTableHead,
} from "@/components/app/projects/project-table";
import { RefreshButton } from "@/components/app/projects/refresh-button";
import { useProjectEditor } from "@/components/app/projects/use-project-editor";
import { Button } from "@/components/ui/button";
import { Heading } from "@/components/ui/heading";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { PaginatedTable } from "@/components/ui/paginated-table";
import { SearchField } from "@/components/ui/search-field";
import { Text } from "@/components/ui/text";
import { paths } from "@/lib/api";
import { useResource } from "@/lib/use-resource";
import { useWorkspace } from "@/lib/workspace";
import { can } from "@/types/workspaces";
import {
  statuses,
  type ProjectQuery,
  type StatusFilter,
} from "@/types/projects";
// The page state lives in the URL (?q=&status=&sort=&page=), so it survives
// reloads and back/forward; every change fetches through SWR.
export default function ProjectIndex() {
  const [search] = useSearchParams();
  const goTo = useNavigate();
  const workspace = useWorkspace();
  const query = projectQuery(search);
  const list = useResource(
    paths.projects(workspace.id).index(indexQuery(query)),
  );
  // Search, filter and sort changes start again from the first page.
  function navigate(change: Partial<ProjectQuery>, replace = false) {
    const path = projectsPath(workspace.id, { ...query, page: 1, ...change });
    void goTo(path, { replace, preventScrollReset: true });
  }
  // A new project sorts first, so show page 1 where it appears.
  const editor = useProjectEditor((_, created) => {
    if (created) navigate({});
  });
  const filtered = Boolean(query.q) || query.status !== "all";
  const canCreate = can(workspace, "project.store");
  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="max-sm:w-full sm:flex-1">
          <Heading>Projects</Heading>
          <Text className="mt-1">
            {list.data
              ? projectCount(list.data.pages.items, filtered)
              : workspace.name}
          </Text>
          <div className="mt-4 flex max-w-xl gap-4">
            <div className="flex-1">
              <SearchField
                label="Search projects"
                value={query.q}
                onSearch={(q) => navigate({ q }, true)}
              />
            </div>
            <div className="w-44">
              <StatusField
                value={query.status}
                onChange={(status) => navigate({ status })}
              />
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <RefreshButton
            refreshing={list.refreshing}
            onRefresh={list.refresh}
          />
          {canCreate && (
            <Button onClick={() => editor.setDialog("new")}>
              <PlusIcon />
              New project
            </Button>
          )}
        </div>
      </div>
      <ProjectNotices error={list.error} onRetry={list.refresh} />
      <PaginatedTable
        className="mt-10"
        page={list.data}
        loading={list.loading}
        label="Projects"
        head={
          <ProjectTableHead
            sort={query.sort}
            sortHref={(sort) =>
              projectsPath(workspace.id, { ...query, page: 1, sort })
            }
          />
        }
        row={(project) => <ProjectRow project={project} />}
        rowKey={(project) => project.id}
        href={(page) => projectsPath(workspace.id, { ...query, page })}
        empty={
          <ProjectEmptyState
            filtered={filtered}
            onCreate={canCreate ? () => editor.setDialog("new") : undefined}
          />
        }
      />
      <ProjectDialog editor={editor} />
    </>
  );
}
function projectCount(items: number, filtered: boolean) {
  const projects = items === 1 ? "project" : "projects";
  return filtered
    ? `${items} matching ${projects}`
    : `${items} ${projects} in this workspace`;
}
function StatusField({
  value,
  onChange,
}: {
  value: StatusFilter;
  onChange: (status: StatusFilter) => void;
}) {
  return (
    <Listbox<StatusFilter>
      aria-label="Filter by status"
      value={value}
      onChange={onChange}
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
  );
}
