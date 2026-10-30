import { ServiceUnavailable } from "@/components/layout/status-page";
// /app has no content of its own: it redirects to the newest workspace's
// dashboard, onboarding or login, and only renders when the API is down.
export { workspaceHomeProps as getServerSideProps } from "@/lib/server";
export default function WorkspaceHome() {
  return <ServiceUnavailable />;
}
