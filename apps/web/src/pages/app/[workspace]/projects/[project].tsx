import { useParams } from "react-router";
import { ChevronLeftIcon, PencilSquareIcon } from "@heroicons/react/16/solid";
import {
  ProjectAvatar,
  ProjectStatusBadge,
  formatDate,
} from "@/components/app/projects/project-display";
import { ProjectDialog } from "@/components/app/projects/project-dialog";
import { ProjectNotices } from "@/components/app/projects/project-notices";
import { useProjectEditor } from "@/components/app/projects/use-project-editor";
import { Button } from "@/components/ui/button";
import {
  DescriptionDetails,
  DescriptionList,
  DescriptionTerm,
} from "@/components/ui/description-list";
import { EmptyState, ListSkeleton } from "@/components/ui/feedback";
import { Heading, Subheading } from "@/components/ui/heading";
import { Link } from "@/components/ui/link";
import { Text } from "@/components/ui/text";
import { parsePublicId } from "@/lib/ids";
import { paths } from "@/lib/api";
import { routes } from "@/lib/routes";
import { useResource } from "@/lib/use-resource";
import { useWorkspace } from "@/lib/workspace";
import type { Project } from "@/types/projects";
// Only well-formed project IDs reach the API; anything else, and a project
// that doesn't exist in this workspace, shows "not found" inside the layout.
export default function ProjectPage() {
  const id = parsePublicId("prj", useParams().project);
  if (id === null) return <ProjectNotFound />;
  return <ProjectContent key={id} id={id} />;
}
function ProjectContent({ id }: { id: string }) {
  const workspace = useWorkspace();
  const project = useResource(paths.projects(workspace.id).show(id));
  const editor = useProjectEditor();
  if (project.missing) return <ProjectNotFound />;
  return (
    <>
      <Link
        href={routes.projects(workspace.id)}
        className="inline-flex items-center gap-2 text-sm/6 text-zinc-500 dark:text-zinc-400"
      >
        <ChevronLeftIcon className="size-4 fill-zinc-400 dark:fill-zinc-500" />
        Projects
      </Link>
      <ProjectNotices
        editor={editor}
        error={project.error}
        onRetry={project.refresh}
      />
      {project.data ? (
        <ProjectDetails
          project={project.data}
          onEdit={() => project.data && editor.setDialog(project.data)}
        />
      ) : (
        project.loading && <ListSkeleton label="Loading project" rows={1} />
      )}
      <ProjectDialog editor={editor} />
    </>
  );
}
function ProjectDetails({
  project,
  onEdit,
}: {
  project: Project;
  onEdit: () => void;
}) {
  return (
    <>
      <div className="mt-4 flex flex-wrap items-center justify-between gap-4 lg:mt-8">
        <div className="flex min-w-0 items-center gap-4">
          <ProjectAvatar project={project} className="size-12" />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
              <Heading className="truncate">{project.name}</Heading>
              <ProjectStatusBadge status={project.status} />
            </div>
            <Text className="mt-1">
              Last updated {formatDate(project.updatedAt, "long")}
            </Text>
          </div>
        </div>
        <Button outline onClick={onEdit}>
          <PencilSquareIcon />
          Edit project
        </Button>
      </div>
      <div className="mt-12">
        <Subheading>Details</Subheading>
        <DescriptionList className="mt-4">
          <DescriptionTerm>Description</DescriptionTerm>
          <DescriptionDetails>
            {project.description || "No description yet"}
          </DescriptionDetails>
          <DescriptionTerm>Status</DescriptionTerm>
          <DescriptionDetails>{project.status}</DescriptionDetails>
          <DescriptionTerm>Created</DescriptionTerm>
          <DescriptionDetails>
            {formatDate(project.createdAt, "long")}
          </DescriptionDetails>
          <DescriptionTerm>Last updated</DescriptionTerm>
          <DescriptionDetails>
            {formatDate(project.updatedAt, "long")}
          </DescriptionDetails>
        </DescriptionList>
      </div>
    </>
  );
}
function ProjectNotFound() {
  const workspace = useWorkspace();
  return (
    <EmptyState
      title="This project has wandered off."
      description="It isn’t in this workspace, or the link is mistyped."
      action={
        <Button href={routes.projects(workspace.id)}>Back to projects</Button>
      }
    />
  );
}
