import { Client, Server } from "styletron-engine-atomic";
export const styletron =
  typeof window === "undefined"
    ? new Server()
    : new Client({
        hydrate: document.getElementsByClassName(
          "_styletron_hydrate_",
        ) as HTMLCollectionOf<HTMLStyleElement>,
      });
