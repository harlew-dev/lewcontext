import { PassThrough } from "node:stream";
import { renderToPipeableStream } from "react-dom/server";
import { ServerRouter, type EntryContext } from "react-router";
// Runs only at build time (ssr: false): it renders the prerendered pages and
// the SPA fallback. React Router's default server entry needs
// @react-router/node, which nothing else here uses, so this is the same
// entry in a few lines.
export default function handleRequest(
  request: Request,
  status: number,
  headers: Headers,
  context: EntryContext,
) {
  return new Promise<Response>((resolve, reject) => {
    const { pipe } = renderToPipeableStream(
      <ServerRouter context={context} url={request.url} />,
      {
        // Wait for the whole page, so the HTML is complete.
        async onAllReady() {
          const html = await collect(pipe);
          headers.set("Content-Type", "text/html; charset=utf-8");
          resolve(new Response(html, { status, headers }));
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
