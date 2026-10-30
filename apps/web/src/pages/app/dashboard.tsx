import {
  ArrowRight,
  ArrowUpRight,
  CheckCheck,
  CircleDashed,
  CircleDot,
  FolderKanban,
} from "lucide-react";
import { ProjectIcon } from "@/components/app/projects/ProjectBadge";
import { ProjectWorkspace } from "@/components/app/projects/ProjectWorkspace";
import { ProjectsSection } from "@/components/app/projects/ProjectsSection";
import { statusBars } from "@/components/app/projects/projectStyles";
import { useProjects } from "@/components/app/projects/useProjects";
import { ServiceUnavailable } from "@/components/ui/Feedback";
import { Panel } from "@/components/ui/Panel";
import { Description, SectionHeading } from "@/components/ui/Typography";
import { cx } from "@/lib/cx";
import {
  statuses,
  type Project,
  type WorkspacePageProps,
  type WorkspaceProps,
} from "@/lib/types";
export { workspaceProps as getServerSideProps } from "@/lib/server";
export default function Dashboard(props: WorkspacePageProps) {
  if ("unavailable" in props) return <ServiceUnavailable />;
  return <DashboardContent {...props} />;
}
function DashboardContent(props: WorkspaceProps) {
  const state = useProjects(props);
  // Summaries are derived from the loaded projects, so hide them until data is ready.
  const ready = !state.loading && !state.error;
  return (
    <ProjectWorkspace
      session={props.session}
      state={state}
      title="Overview"
      eyebrow="YOUR WORK, IN GOOD SHAPE"
      heading={`Welcome back, ${props.session.user.name.split(" ")[0]}.`}
      description="A little clarity for your next big thing. Here’s where everything stands."
      intro={<WelcomeBanner onCreate={() => state.setDialog("new")} />}
    >
      {ready && <Stats projects={state.projects} />}
      <ProjectsSection
        state={state}
        title="Your projects"
        description="A home for everything you’re working on."
        showViewAll
      />
      {ready && (
        <div className="mt-7.75 grid grid-cols-[1fr_1.35fr] gap-5 max-md:gap-3.25 max-sm:grid-cols-1">
          <ProjectHealth projects={state.projects} />
          <Activity projects={state.projects} onEdit={state.setDialog} />
        </div>
      )}
    </ProjectWorkspace>
  );
}
function WelcomeBanner({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="relative mb-6.75 flex h-[231px] justify-between overflow-hidden rounded-[9px] border border-mauve-200 bg-mauve-100 px-7.75 py-7 max-md:h-[225px] max-md:p-6.25 max-sm:h-[215px] max-sm:p-5.5 xl:h-[250px]">
      <div>
        <span className="text-micro font-semibold tracking-[1.5px] text-mauve-700 max-sm:text-fine">
          MAKE SPACE FOR WHAT’S NEXT
        </span>
        <h2 className="relative z-2 mt-3 text-[29px] leading-[1.13] font-medium tracking-[-1px] text-mauve-900 max-lg:text-[27px] max-sm:text-[24px]">
          Big ideas.
          <br />
          Small, steady steps.
        </h2>
        <p className="mt-2.75 text-label leading-[1.65] text-mauve-700 max-sm:text-caption">
          Bring your projects together.
          <br />
          Give your best work room to grow.
        </p>
        <button
          className="mt-4 flex items-center gap-3.75 text-caption font-semibold text-mauve-800"
          onClick={onCreate}
        >
          Start something good <ArrowRight size={16} />
        </button>
      </div>
      <BannerArt />
    </div>
  );
}
// Decorative shapes on the dashboard welcome banner.
function BannerArt() {
  return (
    <div
      className="absolute top-0 right-2.5 h-full w-[48%] min-w-[300px] max-md:-right-10 max-sm:top-5 max-sm:-right-26 max-sm:scale-[0.65] max-sm:opacity-70 xl:right-10"
      aria-hidden="true"
    >
      <div className="absolute top-17.5 left-[9%] h-[135px] w-[330px] -rotate-23 rounded-[50%] border border-mauve-300" />
      <div className="absolute top-1.5 left-[23%] size-55 rotate-30 rounded-full border border-dashed border-mauve-300 opacity-40" />
      <div className="absolute top-15.5 left-[26%] z-2 size-[135px] -rotate-21 rounded-[50%_50%_8%_8%] bg-[linear-gradient(130deg,#baa7ce,#ddcce4)] shadow-art" />
      <div className="absolute top-22.5 left-[52%] z-3 h-29 w-29.75 rotate-17 rounded-[9px] bg-[linear-gradient(130deg,#d9c8b3,#f2e6ce)] shadow-art" />
      <div className="absolute top-5 left-[48%] z-1 size-20.75 -rotate-14 rounded-full bg-[linear-gradient(120deg,#aca7c0,#dfd9ec)] shadow-art" />
      <span className="absolute top-10.75 right-[10%] text-[41px] font-extralight text-[#a392b6]">
        ✳
      </span>
      <span className="absolute bottom-15 left-[17%] size-2.5 rounded-full bg-[#a191b4]" />
      <span className="absolute right-[13%] bottom-3.75 text-fine tracking-[1.6px] text-mauve-500 max-sm:hidden">
        GOOD THINGS ARE TAKING SHAPE.
      </span>
    </div>
  );
}
function Stats({ projects }: { projects: Project[] }) {
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
    <div className="mb-9.25 grid grid-cols-4 rounded-[9px] border border-line max-sm:grid-cols-2">
      {cards.map(({ label, value, caption, icon: Icon }) => (
        <div
          className="flex flex-col border-r border-line px-5.5 py-5 last:border-r-0 max-lg:p-4.25 max-md:p-4 max-sm:p-4.25 max-sm:nth-2:border-r-0 max-sm:nth-[-n+2]:border-b xl:px-6.75 xl:py-5.5"
          key={label}
        >
          <div className="flex items-center justify-between text-label font-medium text-haze-600 max-sm:text-caption">
            {label}
            <Icon size={16} className="text-mauve-600" />
          </div>
          <strong className="mt-4 mb-1.5 text-display leading-[1.2] font-[550] tracking-[-1.1px] max-sm:mt-2.5 max-sm:text-[27px]">
            {String(value).padStart(2, "0")}
          </strong>
          <span className="text-caption text-mauve-600 max-md:text-tiny">
            {caption}
          </span>
        </div>
      ))}
    </div>
  );
}
function ProjectHealth({ projects }: { projects: Project[] }) {
  const count = (status: Project["status"]) =>
    projects.filter((p) => p.status === status).length;
  const percent = projects.length
    ? Math.round((count("Completed") / projects.length) * 100)
    : 0;
  return (
    <Panel>
      <SectionHeading
        compact
        title="The bigger picture"
        description="Every step counts."
      />
      <div className="mt-5.5 mb-4.25 flex items-center gap-3.75">
        <strong className="text-[35px] font-[550] tracking-[-1px]">
          {percent}
          <span className="text-[18px] font-normal text-mauve-500">%</span>
        </strong>
        <span className="text-caption leading-[1.7] text-mauve-600">
          of your projects
          <br />
          across the finish line
        </span>
      </div>
      <div
        className="flex h-1.75 gap-0.75 overflow-hidden rounded-[10px] bg-mauve-100"
        role="img"
        aria-label={`${percent}% completed`}
      >
        {statuses.map((status) => (
          <span
            key={status}
            className={statusBars[status]}
            style={{
              width: `${projects.length ? (count(status) / projects.length) * 100 : 0}%`,
            }}
          />
        ))}
      </div>
      <div className="mt-5 flex flex-col gap-2.75 text-caption text-mauve-600">
        {statuses.map((status) => (
          <div key={status} className="flex justify-between">
            <span className="flex items-center gap-1.75">
              <i className={cx("size-1.5 rounded-[2px]", statusBars[status])} />
              {status}
            </span>
            <strong className="font-medium text-mauve-800">
              {count(status)}
            </strong>
          </div>
        ))}
      </div>
    </Panel>
  );
}
function Activity({
  projects,
  onEdit,
}: {
  projects: Project[];
  onEdit: (project: Project) => void;
}) {
  return (
    <Panel>
      <SectionHeading
        compact
        title="Recently touched"
        description="Pick up where you left off."
      />
      <div>
        {projects.slice(0, 3).map((project) => (
          <button
            key={project.id}
            className="flex w-full items-center gap-3 border-b border-mauve-100 py-4.25 text-left last:border-0"
            onClick={() => onEdit(project)}
          >
            <ProjectIcon project={project} />
            <span>
              <strong className="block text-label font-[550]">
                {project.name}
              </strong>
              <small className="mt-1.5 block text-caption text-mauve-500">
                {project.status === "Completed"
                  ? "Ready for its next chapter"
                  : project.status === "Planned"
                    ? "A good idea, waiting to happen"
                    : "A little closer to the finish line"}
              </small>
            </span>
            <ArrowUpRight size={16} className="ml-auto text-mauve-500" />
          </button>
        ))}
        {projects.length === 0 && (
          <Description className="mt-1.75">
            Your recently updated projects will appear here.
          </Description>
        )}
      </div>
    </Panel>
  );
}
