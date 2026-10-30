import type { AppProps } from "next/app";
import Head from "next/head";
import { Provider as StyletronProvider } from "styletron-react";
import { BaseProvider } from "baseui";
import { SWRConfig } from "swr";
import type { Client, Server } from "styletron-engine-atomic";
import { styletron } from "@/lib/styletron";
import { theme } from "@/lib/theme";
import { request } from "@/lib/api";
import { RouteProgress } from "@/components/ui/RouteProgress";
import "@/styles/globals.css";
import "@/styles/layout.css";
import "@/styles/dashboard.css";
import "@/styles/projects.css";
import "@/styles/components.css";
import "@/styles/auth.css";
import "@/styles/responsive.css";
export default function App({
  Component,
  pageProps,
  engine,
}: AppProps & { engine?: Client | Server }) {
  return (
    <StyletronProvider value={engine || styletron}>
      <BaseProvider theme={theme}>
        <SWRConfig
          value={{
            fetcher: request,
            errorRetryCount: 2,
            revalidateOnFocus: true,
          }}
        >
          <Head>
            <title>Forma — Your work, in good shape.</title>
            <meta
              name="description"
              content="A little clarity for your next big thing. Your projects, together in one thoughtful workspace."
            />
            <meta
              name="viewport"
              content="width=device-width, initial-scale=1"
            />
            <link rel="icon" href="/favicon.svg" />
          </Head>
          <RouteProgress />
          <Component {...pageProps} />
        </SWRConfig>
      </BaseProvider>
    </StyletronProvider>
  );
}
