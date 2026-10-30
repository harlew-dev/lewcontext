import { Client } from "styletron-engine-atomic";
// BaseUI's CSS-in-JS engine in the browser. It starts from the <style
// class="_styletron_hydrate_"> tags that entry.server.tsx wrote into the
// prerendered HTML, so it doesn't add those rules twice.
export const styletron = new Client({
  hydrate: document.getElementsByClassName(
    "_styletron_hydrate_",
  ) as HTMLCollectionOf<HTMLStyleElement>,
});
