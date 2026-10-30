import {
  FolderKanban,
  CircleDot,
  CheckCheck,
  CircleDashed,
} from "lucide-react";
import type { Project } from "@/lib/types";
export function Stats({ projects }: { projects: Project[] }) {
  const cards = [
    {
      label: "Total projects",
      value: projects.length,
      caption: "Ideas taking shape",
      icon: FolderKanban,
    },
    {
      label: "In progress",
      value: projects.filter((p) => p.status === "In progress").length,
      caption: "Moving things forward",
      icon: CircleDot,
    },
    {
      label: "Completed",
      value: projects.filter((p) => p.status === "Completed").length,
      caption: "Good work, well done",
      icon: CheckCheck,
    },
    {
      label: "On the horizon",
      value: projects.filter((p) => p.status === "Planned").length,
      caption: "The next chapter",
      icon: CircleDashed,
    },
  ];
  return (
    <div className="stats-grid">
      {cards.map(({ label, value, caption, icon: Icon }) => (
        <div className="stat-card" key={label}>
          <div className="stat-label">
            {label}
            <Icon size={16} />
          </div>
          <strong className="stat-value">
            {String(value).padStart(2, "0")}
          </strong>
          <span className="stat-caption">{caption}</span>
        </div>
      ))}
    </div>
  );
}
