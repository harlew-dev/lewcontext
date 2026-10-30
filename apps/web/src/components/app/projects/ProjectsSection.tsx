import { useState } from "react";
import { ArrowRight, RefreshCw, Search } from "lucide-react";
import { Input } from "baseui/input";
import { Button } from "@/components/ui/Button";
import { SectionHeading } from "@/components/ui/Typography";
import { TextLink } from "@/components/ui/TextLink";
import { ProjectSkeleton } from "@/components/ui/Feedback";
import { cx } from "@/lib/cx";
import { routes } from "@/lib/routes";
import { statuses } from "@/lib/types";
import { ProjectTable } from "./ProjectTable";
import type { ProjectsState } from "./useProjects";
const allProjects = "All projects";
// Filterable, searchable project list shared by the dashboard and the projects page.
export function ProjectsSection({
  state,
  title,
  description,
  showViewAll = false,
}: {
  state: ProjectsState;
  title: string;
  description: string;
  showViewAll?: boolean;
}) {
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState(allProjects);
  const filtered = state.projects.filter(
    (p) =>
      (status === allProjects || p.status === status) &&
      `${p.name} ${p.description}`.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <section>
      <SectionHeading
        title={title}
        description={description}
        action={
          <div className="flex items-center gap-2.5 max-sm:gap-0">
            <Button
              kind="tertiary"
              isLoading={state.refreshing}
              onClick={state.refresh}
              aria-label="Refresh projects"
            >
              <RefreshCw size={15} />
            </Button>
            {showViewAll && (
              <TextLink href={routes.projects}>
                View all projects <ArrowRight size={14} />
              </TextLink>
            )}
          </div>
        }
      />
      <div className="mb-4 flex items-center justify-between gap-4 max-lg:flex-wrap max-sm:gap-3">
        <StatusFilter
          value={status}
          total={state.projects.length}
          onChange={setStatus}
        />
        <div className="w-[193px] max-lg:w-[190px] max-sm:w-full">
          <Input
            aria-label="Search projects"
            placeholder="Search projects…"
            size="mini"
            value={search}
            onChange={(e) => setSearch(e.currentTarget.value)}
            startEnhancer={<Search size={15} />}
            clearable
          />
        </div>
      </div>
      {state.loading ? (
        <ProjectSkeleton />
      ) : !state.error || state.projects.length > 0 ? (
        <ProjectTable
          projects={filtered}
          onEdit={state.setDialog}
          onCreate={() => state.setDialog("new")}
          filtered={Boolean(search) || status !== allProjects}
        />
      ) : null}
    </section>
  );
}
function StatusFilter({
  value,
  total,
  onChange,
}: {
  value: string;
  total: number;
  onChange: (status: string) => void;
}) {
  return (
    <div
      className="flex items-center gap-1 max-sm:w-full max-sm:justify-between max-sm:gap-0"
      role="group"
      aria-label="Filter projects by status"
    >
      {[allProjects, ...statuses].map((item) => (
        <button
          key={item}
          aria-pressed={value === item}
          className={cx(
            "rounded-md px-2.5 py-1.75 text-caption whitespace-nowrap max-sm:p-1.75 max-sm:text-tiny",
            value === item
              ? "bg-mauve-100 font-semibold text-mauve-900"
              : "text-haze-500",
          )}
          onClick={() => onChange(item)}
        >
          {item}
          {item === allProjects && (
            <span className="ml-1.5 inline-block min-w-4 rounded-[3px] bg-white px-1 py-px text-tiny">
              {total}
            </span>
          )}
        </button>
      ))}
    </div>
  );
}
