import { WorkspacePage } from "@/components/projects/WorkspacePage";
import { workspaceProps } from "@/lib/server";
import type { WorkspaceProps } from "@/lib/types";
export const getServerSideProps = workspaceProps;
export default function Overview(
  props: WorkspaceProps | { unavailable: true },
) {
  return <WorkspacePage {...props} overview />;
}
