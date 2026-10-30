import { ArrowUpRight } from "lucide-react";
import type { Project } from "@/lib/types";
import { ProjectIcon } from "@/components/projects/ProjectBadge";
import { SectionHeading } from "@/components/ui/Typography";
export function Activity({
  projects,
  onEdit,
}: {
  projects: Project[];
  onEdit: (project: Project) => void;
}) {
  return (
    <section className="activity-card">
      <SectionHeading
        title="Recently touched"
        description="Pick up where you left off."
      />
      <div className="activity-list">
        {projects.slice(0, 3).map((project) => (
          <button key={project.id} onClick={() => onEdit(project)}>
            <ProjectIcon project={project} />
            <span>
              <strong>{project.name}</strong>
              <small>
                {project.status === "Completed"
                  ? "Ready for its next chapter"
                  : project.status === "Planned"
                    ? "A good idea, waiting to happen"
                    : "A little closer to the finish line"}
              </small>
            </span>
            <ArrowUpRight size={16} />
          </button>
        ))}
        {projects.length === 0 && (
          <p className="description">
            Your recently updated projects will appear here.
          </p>
        )}
      </div>
    </section>
  );
}
