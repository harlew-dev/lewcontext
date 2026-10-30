import type { AppProps } from "next/app";
import Head from "next/head";
import { Provider as StyletronProvider } from "styletron-react";
import { BaseProvider } from "baseui";
import { SWRConfig } from "swr";
import { MotionConfig } from "motion/react";
import type { Client, Server } from "styletron-engine-atomic";
import { styletron } from "@/lib/styletron";
import { theme } from "@/lib/theme";
import type { NextPageWithLayout } from "@/lib/page";
import { SessionProvider } from "@/lib/session";
import { RouteProgress } from "@/components/ui/route-progress";
import "@/styles/globals.css";
export default function App({
  Component,
  pageProps,
  engine,
}: AppProps & { engine?: Client | Server; Component: NextPageWithLayout }) {
  const getLayout = Component.getLayout ?? ((page) => page);
  return (
    <StyletronProvider value={engine || styletron}>
      <BaseProvider theme={theme}>
        <SWRConfig
          value={{
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
          <MotionConfig reducedMotion="user">
            <SessionProvider session={pageProps.session ?? null}>
              {getLayout(<Component {...pageProps} />, pageProps)}
            </SessionProvider>
          </MotionConfig>
        </SWRConfig>
      </BaseProvider>
    </StyletronProvider>
  );
}
