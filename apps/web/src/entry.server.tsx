import { PassThrough } from "node:stream";
import { renderToPipeableStream } from "react-dom/server";
import { ServerRouter, type EntryContext } from "react-router";
import { Server } from "styletron-engine-atomic";
import { Provider as StyletronProvider } from "styletron-react";
// Runs only at build time (ssr: false): it renders the prerendered pages and
// the SPA fallback. BaseUI's Styletron CSS is collected per file and written
// into <head> as <style class="_styletron_hydrate_">, which the browser's
// engine (lib/styletron.ts) picks up.
export default function handleRequest(
  request: Request,
  status: number,
  headers: Headers,
  context: EntryContext,
) {
  const engine = new Server();
  return new Promise<Response>((resolve, reject) => {
    const { pipe } = renderToPipeableStream(
      <StyletronProvider value={engine}>
        <ServerRouter context={context} url={request.url} />
      </StyletronProvider>,
      {
        // Wait for the whole page, so every style has been collected.
        async onAllReady() {
          const html = await collect(pipe);
          headers.set("Content-Type", "text/html; charset=utf-8");
          const styles = engine.getStylesheetsHtml("_styletron_hydrate_");
          resolve(
            new Response(html.replace("</head>", `${styles}</head>`), {
              status,
              headers,
            }),
          );
        },
        onShellError: reject,
        onError(error) {
          status = 500;
          console.error(error);
        },
      },
    );
  });
}
// The rendered stream as one string.
function collect(pipe: (stream: PassThrough) => void): Promise<string> {
  const stream = new PassThrough();
  const chunks: Buffer[] = [];
  return new Promise((resolve, reject) => {
    stream.on("data", (chunk: Buffer) => chunks.push(chunk));
    stream.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    stream.on("error", reject);
    pipe(stream);
  });
}
