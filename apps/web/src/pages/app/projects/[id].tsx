import type { GetServerSidePropsContext } from "next";
import { ChevronLeftIcon, PencilSquareIcon } from "@heroicons/react/16/solid";
import { appLayout } from "@/components/layout/application-layout";
import { ServiceUnavailable } from "@/components/layout/status-page";
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
import { ListSkeleton } from "@/components/ui/feedback";
import { Heading, Subheading } from "@/components/ui/heading";
import { Link } from "@/components/ui/link";
import { Text } from "@/components/ui/text";
import { parseId } from "@/lib/ids";
import { paths } from "@/lib/api";
import { routes } from "@/lib/routes";
import { appProps, initial } from "@/lib/server";
import type { AppPageProps, Initial, SessionProps } from "@/lib/types";
import { useResource } from "@/lib/use-resource";
import type { Project } from "@/types/projects";
type ProjectProps = SessionProps & { id: number; project: Initial<Project> };
export async function getServerSideProps(context: GetServerSidePropsContext) {
  // Only exact, positive IDs reach the API; anything else is a 404 page.
  const id = parseId(context.params?.id);
  if (id === null) return { notFound: true as const };
  return appProps(context, async (api) => ({
    id,
    project: await initial(
      api,
      paths.projects.show(id),
      "We couldn’t load this project. Please try again.",
    ),
  }));
}
export default function ProjectPage(props: AppPageProps<ProjectProps>) {
  if ("unavailable" in props) return <ServiceUnavailable />;
  return <ProjectContent {...props} />;
}
ProjectPage.getLayout = appLayout;
function ProjectContent({ id, project: initialProject }: ProjectProps) {
  const project = useResource(paths.projects.show(id), initialProject);
  const editor = useProjectEditor();
  return (
    <>
      <Link
        href={routes.projects}
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
