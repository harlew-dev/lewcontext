import { Outlet, useLocation } from "react-router";
import { AdminLayout } from "@/components/layout/admin-layout";
import { PageTransition } from "@/components/layout/page-transition";
import { ServiceUnavailable } from "@/components/layout/status-page";
import { Toaster } from "@/components/ui/toast";
import { AdminProvider } from "@/lib/admin";
import { appName } from "@/lib/app";
import { adminLoader } from "@/lib/loaders";
import type { Route } from "./+types/layout";
// The admin dashboard's chrome around Users and Workspaces. Its loader asks
// the API about the key in the URL first: a wrong one renders the ordinary
// not-found page, and no session goes back to the admin sign-in page. It
// stays mounted between the admin pages, which fetch their own data.
export function clientLoader({ params }: Route.ClientLoaderArgs) {
  return adminLoader(params.key);
}
export function shouldRevalidate() {
  return false;
}
export const meta: Route.MetaFunction = () => [
  { title: `Admin · ${appName}` },
  { name: "robots", content: "noindex, nofollow" },
];
export default function AdminLayoutRoute({
  loaderData,
  params,
}: Route.ComponentProps) {
  const { pathname } = useLocation();
  if ("unavailable" in loaderData) return <ServiceUnavailable />;
  return (
    <AdminProvider adminKey={params.key} session={loaderData.session}>
      <AdminLayout>
        <PageTransition key={pathname}>
          <Outlet />
        </PageTransition>
      </AdminLayout>
      <Toaster />
    </AdminProvider>
  );
}
