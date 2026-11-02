<img src="lewcontext.png" alt="" width="56" height="56">

# lewcontext

**Context for AI agents, built from the documents you already have.** lewcontext turns scattered, unstructured files into a structured, project-specific wiki that agents can search and reason over.

> [!WARNING]
> **Under development.** lewcontext is not ready for use yet. This README describes where it is headed: the retrieval and graph features are not built. What runs today is the application they will live in.

## Contents

- [Features](#features)
- [How it works](#how-it-works)
  - [Ingestion pipeline](#ingestion-pipeline)
  - [Knowledge wiki](#knowledge-wiki)
  - [Canonical knowledge graph](#canonical-knowledge-graph)
  - [Agent graph tools](#agent-graph-tools)
  - [Live graph view](#live-graph-view)
- [Use cases](#use-cases)
- [The app](#the-app)
  - [Getting started](#getting-started)
  - [Development](#development)
  - [Testing and checks](#testing-and-checks)
  - [Architecture](#architecture)
  - [Security](#security)
  - [Deployment](#deployment)
- [Documentation](#documentation)

## Features

- **A wiki built from your files.** Use the documents you already have. Background AI agents extract scattered, unstructured files and rewrite them into a wiki of Markdown pages, with references to the images, videos and files they draw on.
- **Retrieval.** Context-aware chunking, RAG, graph RAG, full-text search and reranking.
- **Reasoning over MCP.** LLM reasoning over what it finds, served to your agents over MCP.
- **Live graph.** Every search drawn live across the graph: entities, links and sources.

## How it works

lewcontext rewrites unstructured documents into structured knowledge: a wiki for people and agents to read, and a knowledge graph to retrieve from.

```
Unstructured files       report.pdf · notes.docx · slides.pptx · kickoff.mp4
        │
        ▼
Ingestion pipeline       extract → chunk → contextualize → build graph
        │
        ▼
Background AI agents     rewrite the knowledge into wiki pages, citing sources
        │
        ▼
Structured knowledge     wiki: Markdown pages with image, video and file references
                         knowledge graph: entities, facts and communities
```

### Ingestion pipeline

1. **Document extraction** with [Docling](https://github.com/docling-project/docling).
2. **Length-aware semantic chunking** using an LLM.
3. **Contextualized chunking.** Each chunk is rewritten by an LLM to include context from the chunks before it, for better retrieval.
4. **Graph construction.** Entities, relationships, facts and communities are extracted into the canonical knowledge graph.
5. **Wiki writing.** Background AI agents rewrite the whole body of knowledge into a wiki of Markdown pages, organised by topic rather than by source file.

### Knowledge wiki

The wiki is the structured version of your documents: plain Markdown pages that people and agents can read, with references to the images, videos and files behind them.

```
wiki/
├── index.md
├── projects/
│   └── apollo.md
├── organisations/
│   ├── acme.md
│   └── division-x.md
├── assets/
│   ├── apollo-architecture.png
│   └── kickoff.mp4
└── sources/
    ├── report.pdf
    └── notes.docx
```

Each page draws together what every source says about one topic and cites where it came from:

```markdown
# Project Apollo

Apollo is owned by [Division X](../organisations/division-x.md), which buys
from [Acme](../organisations/acme.md).[^1]

![Apollo architecture](../assets/apollo-architecture.png)

[^1]: [report.pdf, section 3.2](../sources/report.pdf)
```

### Canonical knowledge graph

The knowledge graph holds the same knowledge in a form agents can search and traverse. Nothing loses its source along the way: every chunk links back to the original document and the section it came from, and every fact links to the chunks that support it. Any answer can be traced back to the file it came from:

```
Answer ──▶ Fact ── EVIDENCED_BY ──▶ Chunk ◀── HAS_CHUNK ── Document ──▶ report.pdf, section 3.2
```

All of it lives in one graph:

```
Document
   │
   ├── HAS_CHUNK ─────▶ Chunk
   │                      │
   │                      ├── MENTIONS ───▶ Entity
   │                      │
   │                      └── SUPPORTS ───▶ Fact
   │
   └── BELONGS_TO ─────▶ Corpus

Entity ── RELATIONSHIP ──▶ Entity

Fact ─── SUBJECT ─────────▶ Entity
Fact ─── OBJECT ──────────▶ Entity
Fact ─── EVIDENCED_BY ───▶ Chunk

Entity ── MEMBER_OF ──────▶ Community

Community ── CHILD_OF ────▶ Community
Community ── HAS_SUMMARY ─▶ CommunityReport
```

### Agent graph tools

Agents connect over MCP and explore the graph through a small set of tools. Each call is traced and drawn live on the graph.

| Tool | Purpose |
| --- | --- |
| `search_graph(query, node_types, top_k)` | Find candidate nodes for a query |
| `expand_nodes(node_ids, relationship_types, hops, limit)` | Walk outward from nodes along relationships |
| `get_nodes(node_ids)` | Fetch full node content |
| `search_community(query, depth)` | Search community reports at a given level |
| `find_path(source_id, target_id, max_hops)` | Find how two nodes are connected |
| `search_facts(query, entities, date_range)` | Find facts, optionally scoped to entities and dates |

For example, asked _"How is project Apollo connected to Acme?"_, an agent might run:

```
search_graph("project Apollo Acme")
        ↓
[Apollo]  [Acme]  [Bob Smith]
        ↓
expand_nodes(["Apollo", "Acme"], hops=2)
        ↓
[Apollo] ── owned_by ──▶ [Division X] ── supplier ──▶ [Acme]
        ↓
get_nodes(...)
        ↓
LLM produces the answer
```

### Live graph view

Every tool call is drawn over the graph as it runs. The graph itself does not change; the trace is overlaid on it:

```
Before                         During execution

          ○                              ○
    ○─────●────○                   ○─────🟢═══🟡
   /       \                      /       ║
  ○         ○────○               ○        🟢────○
           /                              ║
      ○───○                          ○───🟣
```

Node colours show how far each node got through retrieval:

| Colour | Meaning |
| --- | --- |
| Grey | In the graph, untouched |
| Blue | Retrieval candidate |
| Purple | Graph traversal passed through it |
| Yellow | Selected by the reranker |
| Green | Actually exposed to the LLM |
| Orange | Referenced or cited in the final answer |

A **bright pulse** marks nodes touched during the current operation.

The trace can be viewed two ways:

| View | Shows |
| --- | --- |
| **Full graph** | The whole corpus, with the live trace overlaid |
| **Discovered** | Only the subgraph the agent has encountered so far, built up live |

## Use cases

- **Project context for agents**, drawn from your existing documents.
- **Debugging retrieval:** why did the agent miss that document?
- **Understanding agent behaviour:** which communities and entities does it lean on?
- **Demos and teaching:** show how GraphRAG works, visually and live.

## The app

The retrieval and graph features are being built into a working project workspace with a Go API, PostgreSQL, and a React single-page app with a prerendered public homepage. The UI is built with Tailwind (sidebar layout, auth layout, buttons, fields, listbox, dialog, dropdown, table) on **Uber Base Web (BaseUI)** for behaviour, with light and dark mode.

It is a working app, not a mockup. The dashboard shows real database data: you can create and edit projects, change their status, search and filter them, manage other sessions and sign out. Data persists across reloads, and every account can access only its own projects.

### Getting started

There are two ways to run it: entirely in Docker, or with Go and Node on your machine and only the database in Docker.

#### Run in Docker

**Requirements:** Docker with Compose.

```sh
cp .env.example .env
make docker-dev
```

This starts PostgreSQL (with pgvector), builds the app from the `Dockerfile`, migrates and seeds the database, then starts the app. Run it again after pulling changes: it rebuilds the image, and migrating and seeding are safe to repeat.

Open **http://localhost:3000** and sign in as described in step 4 below. `make docker-down` stops the containers and keeps the data. The image builds the web app with Node and the server with Go, so neither is needed on your machine.

#### Run for development

**Requirements:** Go 1.26+, Node 24+, npm and Docker with Compose (or your own PostgreSQL 15+ with pgvector).

1. **Start the database and set it up.**

   ```sh
   cp .env.example .env
   make db
   make install
   make migrate
   make seed
   ```

   `make db` starts PostgreSQL 17 with pgvector from `compose.yaml`, on `127.0.0.1:5432` with the credentials in `.env.example`, which are for local development only. Setup never drops existing tables or data, and the seed runs only when invoked, refuses to run in production, and leaves an existing demo account unchanged.

2. **Start the dev server** from the repository root.

   ```sh
   make dev
   ```

   This (`scripts/dev.sh`) rebuilds the web app the API embeds, then runs the Go API on `:8080` and Vite on `:3000` in one terminal, with Vite forwarding `/api` to the API. Ctrl-C stops both. See [running and building](docs/testing.md#run-the-dev-server).

3. **Open http://localhost:3000.** Use this exact host, not `127.0.0.1`: the default allowed origin is `http://localhost:3000`.

4. **Sign in** as `test@example.com` with the `DEMO_PASSWORD` from `.env` (`password` in the example), or choose **Sign up** to start with your own empty workspace.

#### Sample data

Extra seeders insert random fake data (gofakeit) for trying pagination, search and sorting. They only insert, into the `DATABASE_URL` in `.env`:

```sh
make seed only=volume count=500           # projects for the demo account (or email=you@example.com)
make seed only=users count=25 projects=20 # fake accounts with up to 20 projects each, password DEMO_PASSWORD
```

Fake records come from the factories in `apps/api/internal/factory`, one per table. Integration tests use the same factories.

### Development

| Command | Does |
| --- | --- |
| `make db` | Start PostgreSQL (with pgvector) in Docker |
| `make dev` | API and Vite (the app's, and the admin dashboard's behind it) together in one terminal (or `make api`, `make web` and `make web-admin` apart) |
| `make serve` | Build the production binary and run it on `:3000` |
| `make migrate` | Apply pending migrations |
| `make migrate-status` | List migrations and whether they are applied |
| `make migration name=add_tasks` | Create an up/down migration pair |
| `make migrate-rollback` | Preview undoing the last migration group; `CONFIRM=1` runs it and can drop data |
| `make seed` | Seed the demo account (see [Sample data](#sample-data) for other seeders) |
| `make docker-dev` | Start the whole app in Docker on `:3000`: database, build, migrate, seed, app |
| `make docker-app` | After a code change (UI or API): rebuild the image and restart only the app container, without migrating or seeding |
| `make docker-up` / `make docker-down` | Build and start, or stop, the whole app in Docker without migrating or seeding |
| `make docker-migrate` / `make docker-seed` | Migrate or seed from the app container |

Configuration lives in `.env` (see [configuration](docs/testing.md#configuration)). `APP_NAME` sets the product name used in page titles, the logo and app copy (`lewcontext` by default); it is baked into the web build, so rebuild after changing it.

Migrations use Bun's migrator: explicit, versioned, transactional per file, grouped for rollback, and protected by a PostgreSQL advisory lock. They never run automatically on API startup.

### Testing and checks

```sh
make db       # PostgreSQL, and the tests' database (lewcontext_test)
make test     # Go unit and PostgreSQL integration tests (race detector), React and API client tests
make check    # gofmt, go vet, TypeScript, ESLint, Prettier
make build    # both web builds (app and admin), embedded into one Go executable (bin/api)
make test-e2e # end-to-end tests in Chromium, against the production binary
```

Tests use the committed `.env.testing` and nothing from `.env`, however they're started. **Integration tests** run against a real PostgreSQL database, `lewcontext_test`, each in a fresh temporary schema that is removed afterwards; `make test` fails if the database isn't running.

**End-to-end tests** build the single binary and start it on :3100 against a database of their own (`lewcontext_test_3100`), so they run beside `make dev`. Another run at the same time takes another port and database: `make test-e2e E2E_PORT=auto`. They cover registration, login failures, project creation and editing, pending states, filters, the prerendered homepage, the `/app` session redirect, CSP hashes (no violations), security headers, mobile layout, session revocation, logout and persistence. Chromium defaults to `/usr/bin/chromium`; set `CHROMIUM_PATH` to use another. See the [test environment guide](docs/testing.md).

### Architecture

In production, one Go process serves both the API and the web app from the same origin: no proxy and no Node runtime. Pages render in the browser. The signed-in layout loads the session once and pages fetch their data through SWR with skeletons, so moving between pages is instant. BaseUI's Styletron CSS for prerendered pages is collected at build time and hydrated on the client.

The app shell and API responses are never cached, while hashed assets are cached for a year. SWR caches are keyed by user ID, so data never carries across a change of account.

**API (`apps/api`)**

| Path | Contents |
| --- | --- |
| `cmd` | Server, migration and seed entrypoints |
| `internal/httpapi/router/routes.go` | Every API route and its authentication requirement, plus the web app's page routes (signed-out `/app/*` requests go to login before any HTML is served) |
| `internal/{auth,config,database,httpapi,store}` | Cryptography, validated configuration, Bun connection and migrator, HTTP boundary (the core, with controllers in `httpapi/endpoints/<area>`), Bun models and queries |
| `internal/web` | Serves the embedded web build: prerendered pages, the single-page-app fallback (404 for unknown URLs), cached assets, security headers, and a CSP with each HTML file's inline-script hashes; and the admin dashboard's own build, only behind its key and assets pass |
| `migrations` | Embedded Bun SQL migrations (`.tx.up.sql` / `.tx.down.sql` pairs) |

**Web (`apps/web`)**

| Path | Contents |
| --- | --- |
| `src/routes.ts` | React Router page registry. `react-router.config.ts` prerenders `/` and builds everything else as a single-page app; the admin dashboard is a second build (`WEB_BUILD=admin`) |
| `src/pages` | Pages, nested as `home/index.tsx` (`/`), `auth/` (`/auth/login`, `/auth/register`) and `app/` (`/app/onboarding`, `/app/settings`, `/app/wsp_…/dashboard`, `/app/wsp_…/projects`, …); every page is a folder with the page in its `index.tsx`, a record's page `[<param>].tsx`. Each page holds its own content; `app/layout.tsx` is the signed-in layout route |
| `src/components/ui` | Tailwind components in kebab-case files (`button.tsx`, `input.tsx`, `fieldset.tsx`, `listbox.tsx`, `dialog.tsx`, `dropdown.tsx`, `sidebar.tsx`, `navbar.tsx`, `table.tsx`, …), with BaseUI underneath where there is behaviour |
| `src/components/layout` | `sidebar-layout.tsx`, `auth-layout.tsx`, `application-layout.tsx` (the app's sidebar, navbar and account menu) and the status page |
| `src/components/app/projects` | Project UI shared by the dashboard and projects pages |
| `src/lib` | Typed API client, page URLs (`routes.ts`), route loaders, BaseUI theme and `slot()` override helper, interaction-state hook, Styletron setup, domain types |
| `src/styles` | Tailwind entry (`globals.css`, which also loads Inter), font setting (`theme.css`) and element defaults (`base.css`). Colours and spacing are Tailwind's defaults |

### Security

- **Passwords** are hashed with Argon2id.
- **Sessions** use 256-bit opaque random tokens, stored as SHA-256 digests, in HttpOnly/SameSite cookies. They expire after 30 minutes idle or 24 hours in total, rotate at login, are revoked at logout, and are limited to five per account.
- **Mutations** require an exact trusted `Origin`, a JSON content type and an authenticated CSRF token.
- **Rate limits:** auth endpoints have persistent IP and account limits and bounded hashing concurrency; every signed-in request has a budget per user, creating workspaces and projects has its own limit, and quotas cap how many can exist ([details](docs/api/rate-limits.md)).

See [security details and limits](docs/api/auth.md).

### Deployment

For a production-like run locally, use `make serve` (or `make build`, then run `./bin/api` with the configuration exported). `bin/api` refuses to start in production without the web build embedded, and anywhere with `ADMIN_ROUTE_KEY` set but no admin build.

Before deploying to production:

- Set `APP_ENV=production` and an HTTPS `APP_ORIGIN`.
- Use a restricted database role with `sslmode=verify-full`.
- Terminate TLS at a trusted reverse proxy and expose only the Go server behind it. The production session cookie uses the `__Host-` prefix and the Secure flag. Set `CLIENT_IP_HEADER` and `TRUSTED_PROXIES` so per-IP limits and events see the real client address ([deployment](docs/deployment.md#behind-a-proxy-or-load-balancer)).
- Do not use the demo credentials.

> [!IMPORTANT]
> lewcontext does not implement verified email, MFA or OIDC federation. Password reset links and workspace invitations are emailed over SMTP; locally they land in Mailpit at `http://localhost:8025`.

## Documentation

How each part of the system works is documented under [`docs`](docs/README.md):

- [`docs/api`](docs/api/README.md): endpoints, authentication, pagination, database, seeding and tests.
- [`docs/ui`](docs/ui/README.md): pages, API use, pagination, design and tests.
- [`docs/testing.md`](docs/testing.md): test environment and manual checks.
