import { startTransition, StrictMode } from "react";
import { hydrateRoot } from "react-dom/client";
import { HydratedRouter } from "react-router/dom";
import { Provider as StyletronProvider } from "styletron-react";
import { styletron } from "@/lib/styletron";
startTransition(() => {
  hydrateRoot(
    document,
    <StrictMode>
      <StyletronProvider value={styletron}>
        <HydratedRouter />
      </StyletronProvider>
    </StrictMode>,
  );
});
