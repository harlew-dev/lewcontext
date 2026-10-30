import { useState } from "react";
import Link from "next/link";
import {
  Plus,
  ArrowRight,
  RefreshCw,
  Search,
  X,
  CheckCircle2,
} from "lucide-react";
import { Input } from "baseui/input";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/Button";
import { PageHeading, SectionHeading } from "@/components/ui/Typography";
import {
  ErrorNotice,
  ProjectSkeleton,
  ServiceUnavailable,
} from "@/components/ui/Feedback";
import { ProjectTable } from "./ProjectTable";
import { ProjectDialog } from "./ProjectDialog";
import { useProjects } from "./useProjects";
import { WelcomeBanner } from "@/components/dashboard/WelcomeBanner";
import { Stats } from "@/components/dashboard/Stats";
import { ProjectHealth } from "@/components/dashboard/ProjectHealth";
import { Activity } from "@/components/dashboard/Activity";
import { statuses, type WorkspaceProps } from "@/lib/types";
import { routes } from "@/lib/routes";
export function WorkspacePage(
  props: (WorkspaceProps | { unavailable: true }) & { overview?: boolean },
) {
  if ("unavailable" in props) return <ServiceUnavailable />;
  return <WorkspaceContent {...props} overview={props.overview ?? false} />;
}
function WorkspaceContent(props: WorkspaceProps & { overview: boolean }) {
  const state = useProjects(props);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("All projects");
  const filtered = state.projects.filter(
    (p) =>
      (status === "All projects" || p.status === status) &&
      `${p.name} ${p.description}`.toLowerCase().includes(search.toLowerCase()),
  );
  const createButton = (
    <Button
      onClick={() => state.setDialog("new")}
      startEnhancer={() => <Plus size={16} />}
    >
      New project
    </Button>
  );
  return (
    <AppShell
      session={props.session}
      title={props.overview ? "Overview" : "Projects"}
    >
      <PageHeading
        eyebrow={
          props.overview ? "YOUR WORK, IN GOOD SHAPE" : "A PLACE FOR YOUR IDEAS"
        }
        title={
          props.overview
            ? `Welcome back, ${props.session.user.name.split(" ")[0]}.`
            : "Your projects."
        }
        description={
          props.overview
            ? "A little clarity for your next big thing. Here’s where everything stands."
            : "From the first spark to the final detail. Keep it all together."
        }
        action={createButton}
      />
      {state.notice && (
        <div role="status" className="success-notice">
          <CheckCircle2 size={16} />
          {state.notice}
          <button
            className="icon-button"
            aria-label="Dismiss notification"
            onClick={() => state.setNotice("")}
          >
            <X size={14} />
          </button>
        </div>
      )}
      {props.overview && (
        <WelcomeBanner onCreate={() => state.setDialog("new")} />
      )}
      {state.error && (
        <ErrorNotice message={state.error} retry={state.refresh} />
      )}
      {props.overview && !state.loading && !state.error && (
        <Stats projects={state.projects} />
      )}
      <section className="projects-section">
        <SectionHeading
          title={props.overview ? "Your projects" : "All the moving pieces"}
          description={
            props.overview
              ? "A home for everything you’re working on."
              : `${state.projects.length} projects in your workspace`
          }
          action={
            <div className="section-actions">
              <Button
                kind="tertiary"
                isLoading={state.refreshing}
                onClick={state.refresh}
                aria-label="Refresh projects"
              >
                <RefreshCw size={15} />
              </Button>
              {props.overview && (
                <Link className="text-link" href={routes.projects}>
                  View all projects <ArrowRight size={14} />
                </Link>
              )}
            </div>
          }
        />
        <div className="project-controls">
          <div
            className="filter-tabs"
            role="group"
            aria-label="Filter projects by status"
          >
            {["All projects", ...statuses].map((item) => (
              <button
                key={item}
                aria-pressed={status === item}
                className={status === item ? "selected" : ""}
                onClick={() => setStatus(item)}
              >
                {item}
                {item === "All projects" && (
                  <span>{state.projects.length}</span>
                )}
              </button>
            ))}
          </div>
          <div className="search-field">
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
            filtered={Boolean(search) || status !== "All projects"}
          />
        ) : null}
      </section>
      {props.overview && !state.loading && !state.error && (
        <div className="dashboard-bottom">
          <ProjectHealth projects={state.projects} />
          <Activity projects={state.projects} onEdit={state.setDialog} />
        </div>
      )}
      {state.dialog && (
        <ProjectDialog
          project={state.dialog === "new" ? undefined : state.dialog}
          csrfToken={props.session.csrfToken}
          onClose={() => state.setDialog(null)}
          onSaved={state.saved}
        />
      )}
    </AppShell>
  );
}
