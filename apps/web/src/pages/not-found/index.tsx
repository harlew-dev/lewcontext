import { NotFound } from "@/components/layout/status-page";
// Every URL no other route matches. Go answers these with status 404.
export default function NotFoundPage() {
  return <NotFound />;
}
