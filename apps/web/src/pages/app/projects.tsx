import { ProjectWorkspace } from "@/components/app/projects/ProjectWorkspace";
import { ProjectsSection } from "@/components/app/projects/ProjectsSection";
import { useProjects } from "@/components/app/projects/useProjects";
import { ServiceUnavailable } from "@/components/ui/Feedback";
import type { WorkspacePageProps, WorkspaceProps } from "@/lib/types";
export { workspaceProps as getServerSideProps } from "@/lib/server";
export default function Projects(props: WorkspacePageProps) {
  if ("unavailable" in props) return <ServiceUnavailable />;
  return <ProjectsContent {...props} />;
}
function ProjectsContent(props: WorkspaceProps) {
  const state = useProjects(props);
  return (
    <ProjectWorkspace
      session={props.session}
      state={state}
      title="Projects"
      eyebrow="A PLACE FOR YOUR IDEAS"
      heading="Your projects."
      description="From the first spark to the final detail. Keep it all together."
    >
      <ProjectsSection
        state={state}
        title="All the moving pieces"
        description={`${state.projects.length} projects in your workspace`}
      />
    </ProjectWorkspace>
  );
}
