import type { GetServerSidePropsContext, GetServerSidePropsResult } from "next";
import type { Project, Session, WorkspaceProps } from "./types";
import { apiRoutes, routes } from "./routes";
const apiURL = process.env.API_URL || "http://127.0.0.1:8080";
export async function workspaceProps(
  context: GetServerSidePropsContext,
): Promise<GetServerSidePropsResult<WorkspaceProps | { unavailable: true }>> {
  context.res.setHeader("Cache-Control", "private, no-store, max-age=0");
  const options = {
    headers: { cookie: context.req.headers.cookie || "" },
    signal: AbortSignal.timeout(8000),
  };
  try {
    const response = await fetch(apiURL + apiRoutes.me, options);
    if (response.status === 401)
      return { redirect: { destination: routes.login, permanent: false } };
    if (!response.ok) throw new Error("Session service unavailable");
    const session: Session = await response.json();
    try {
      const projectsResponse = await fetch(apiURL + apiRoutes.projects, {
        ...options,
        signal: AbortSignal.timeout(8000),
      });
      if (!projectsResponse.ok) throw new Error("Unable to load projects");
      const initialProjects: Project[] = await projectsResponse.json();
      return { props: { session, initialProjects, initialError: null } };
    } catch {
      return {
        props: {
          session,
          initialProjects: null,
          initialError: "We couldn’t load your projects. Please try again.",
        },
      };
    }
  } catch {
    context.res.statusCode = 503;
    return { props: { unavailable: true } };
  }
}
