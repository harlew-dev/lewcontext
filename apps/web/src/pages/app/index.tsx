import { ServiceUnavailable } from "@/components/layout/status-page";
import { workspaceHomeLoader } from "@/lib/loaders";
import type { Route } from "./+types/index";
// /app has no content of its own: its loader redirects to the newest
// workspace's dashboard, onboarding or login, so this only renders when the
// API is down.
export function clientLoader({ request }: Route.ClientLoaderArgs) {
  return workspaceHomeLoader(request);
}
export default function WorkspaceHome({ loaderData }: Route.ComponentProps) {
  return <ServiceUnavailable requestId={loaderData.requestId} />;
}
