import { redirectDocument, type ClientLoaderFunctionArgs } from "react-router";
import { NotFound } from "@/components/layout/status-page";
// The admin build's every-other-URL page. Its routes are only the admin
// dashboard's, so a link from it to the app (Back to app, the not-found
// page's own button) lands here: anything outside /admin/ loads as a new
// page, which Go answers from the app build. Go only ever serves this
// build at /admin/<key>…, so that can't loop. An unknown admin URL is the
// ordinary not-found page. (The sign-in page's logo link is its own real
// page load: reloadDocument.)
export function clientLoader({ request }: ClientLoaderFunctionArgs) {
  const url = new URL(request.url);
  if (!url.pathname.startsWith("/admin/"))
    throw redirectDocument(url.pathname + url.search + url.hash);
  return null;
}
export default function AdminNotFound() {
  return <NotFound />;
}
