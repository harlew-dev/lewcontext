import type { NextPage } from "next";
import type { ReactElement, ReactNode } from "react";
// A page can supply a layout that _app keeps mounted across navigations
// (Next.js per-page layouts), so the sidebar persists and can animate.
export type NextPageWithLayout<P = Record<string, unknown>> = NextPage<P> & {
  getLayout?: (page: ReactElement, props: P) => ReactNode;
};
