import type { Project } from "@/lib/types";
import { statuses } from "@/lib/types";
import { SectionHeading } from "@/components/ui/Typography";
export function ProjectHealth({ projects }: { projects: Project[] }) {
  const completed = projects.filter((p) => p.status === "Completed").length;
  const percent = projects.length
    ? Math.round((completed / projects.length) * 100)
    : 0;
  return (
    <section className="health-card">
      <SectionHeading
        title="The bigger picture"
        description="Every step counts."
      />
      <div className="health-summary">
        <strong>
          {percent}
          <span>%</span>
        </strong>
        <span>
          of your projects
          <br />
          across the finish line
        </span>
      </div>
      <div
        className="progress-track"
        role="img"
        aria-label={`${percent}% completed`}
      >
        {statuses.map((status) => (
          <span
            key={status}
            className={`progress-${status.toLowerCase().replace(" ", "-")}`}
            style={{
              width: `${projects.length ? (projects.filter((p) => p.status === status).length / projects.length) * 100 : 0}%`,
            }}
          />
        ))}
      </div>
      <div className="health-legend">
        {statuses.map((status) => (
          <div key={status}>
            <span>
              <i
                className={`legend-${status.toLowerCase().replace(" ", "-")}`}
              />
              {status}
            </span>
            <strong>
              {projects.filter((p) => p.status === status).length}
            </strong>
          </div>
        ))}
      </div>
    </section>
  );
}
