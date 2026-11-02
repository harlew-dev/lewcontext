# React StrictMode

`<StrictMode>` is a React component that renders nothing itself and switches on extra checks for everything inside it, **in development only**. lewcontext wraps the whole app in it in `apps/web/src/entry.client.tsx`:

```tsx
<StrictMode>
  <StyletronProvider value={styletron}>
    <HydratedRouter />
  </StyletronProvider>
</StrictMode>
```

Keep it on. It costs production nothing and catches real bugs early.

## What it does (React 18)

It does some things twice on purpose, so that bugs which would otherwise be rare show up at once:

| Check | What happens | The bug it finds |
| --- | --- | --- |
| Double render | Each component function runs twice per render, as do `useState`/`useMemo`/`useReducer` initialisers and reducers. One result is thrown away. | Rendering with side effects: changing a module variable, counting calls, mutating props. Rendering must be pure. |
| Mount, unmount, mount | When a component first appears, React runs every effect's setup, then its cleanup, then the setup again. Class components get `componentDidMount`, `componentWillUnmount`, `componentDidMount`. State and refs are kept across this. | Effects without matching cleanup: listeners added twice, timers left running, subscriptions leaked. React also needs components to survive being hidden and shown again with their state kept. |
| Deprecation warnings | Logs warnings for outdated APIs (`findDOMNode`, string refs, legacy context). | Code that will break in later React versions. |

For example, this effect has no cleanup:

```tsx
useEffect(() => {
  window.addEventListener("resize", onResize);
  // missing: return () => window.removeEventListener("resize", onResize);
}, []);
```

Without StrictMode the missing cleanup might never be noticed. With it the listener is added twice in dev, so the bug shows at once.

## Where it applies

| Where | StrictMode? |
| --- | --- |
| `make dev` / `make web` / `make web-admin` (Vite on :3000, the admin pages' on :3002) | **Yes** |
| `make build`, `make serve`, production | No (React drops it from production builds) |
| Vitest (React Testing Library's `render`) | No (it isn't configured to wrap tests in StrictMode) |
| Playwright e2e (`make test-e2e`, against `make serve`) | No |

So only the dev server shows StrictMode behaviour. Something that happens only under `make dev` may be StrictMode, and the production build is the place to judge it.

## What it means for our code

- **Every effect cleans up** what it set up: timers (`RouteProgress`), listeners, scroll locks (`lib/scroll-lock.ts`), focus. Code that's correct under the mount/unmount/mount cycle is correct when a component really unmounts and comes back.
- **Don't count on an effect running once.** Data loads through route loaders and SWR (`useResource`), not fetches in `useEffect`, so dev doesn't send duplicate requests from effects.
- **Refs survive the remount** (React 18 keeps state and refs), so guards like `useAction`'s `running` ref keep working.
- **Measure timing and animation on the production build.** Dev adds an extra render and remount on first appearance, which can change what you see in the first frames.

## Side effects in libraries

A library that isn't written for the mount, unmount, mount cycle can behave differently in dev. Known cases:

- **BaseUI dialogs don't animate open under `make dev`** (production is fine). BaseUI's `Layer` creates a new container on the second mount, so React rebuilds the dialog after it has already opened. See [base-ui.md](base-ui.md#dialogs-dont-animate-open-in-dev).
