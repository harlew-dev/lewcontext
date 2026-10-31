import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { MagnifyingGlassIcon, PlusIcon } from "@heroicons/react/16/solid";
import { ProjectDialog } from "@/components/app/projects/project-dialog";
import { ProjectNotices } from "@/components/app/projects/project-notices";
import {
  cleanSearch,
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
import { Input, InputGroup } from "@/components/ui/input";
import { Listbox, ListboxLabel, ListboxOption } from "@/components/ui/listbox";
import { PaginatedTable } from "@/components/ui/paginated-table";
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
export default function Projects() {
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
// Search box that updates the URL once typing pauses. It follows the URL when
// that changes elsewhere (back/forward), without discarding text being typed.
function SearchField({
  value,
  onSearch,
}: {
  value: string;
  onSearch: (q: string) => void;
}) {
  const [draft, setDraft] = useState(value);
  const [shown, setShown] = useState(value);
  // The last search this field sent; the URL catching up to it is no reason
  // to overwrite the draft.
  const [sent, setSent] = useState(value);
  if (value !== shown) {
    setShown(value);
    if (value !== sent) {
      setDraft(value);
      setSent(value);
    }
  }
  const search = useRef(onSearch);
  useEffect(() => {
    search.current = onSearch;
  });
  useEffect(() => {
    const q = cleanSearch(draft);
    if (q === value) return;
    const timer = setTimeout(() => {
      setSent(q);
      search.current(q);
    }, 300);
    return () => clearTimeout(timer);
  }, [draft, value]);
  return (
    <InputGroup>
      <MagnifyingGlassIcon />
      <Input
        type="search"
        aria-label="Search projects"
        placeholder="Search projects…"
        maxLength={100}
        value={draft}
        onChange={(e) => setDraft(e.currentTarget.value)}
      />
    </InputGroup>
  );
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
