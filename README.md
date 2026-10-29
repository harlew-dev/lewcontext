# lewcontext — Live GraphRAG Visualization

**Watch your agent think.** lewcontext renders a GraphRAG knowledge graph as a living, neural-network-style map and lights up the nodes an agent touches as it retrieves context, in real time.

> **Status:** early planning. Nothing here is built yet.

## Why

RAG and GraphRAG pipelines are usually black boxes. You see the final answer, but not which documents, entities, facts or communities the agent looked at to get there. lewcontext makes retrieval observable: every step the agent takes is mapped onto the graph and shown as it happens.

## Features

- **Full graph preview.** The whole knowledge graph drawn as an interactive network: documents, chunks, entities, facts, communities and their reports.
- **Live graph monitoring.** Nodes and edges light up the moment the agent touches them, like activations firing in a neural network, so you can follow its reasoning path.
- **Discovered-nodes view.** A second view that starts empty and only shows the nodes the agent has actually seen, so its picture of the knowledge base grows as it explores.
- **Trace overlay, not mutation.** The graph itself never changes during a run. The execution trace is layered on top of it.

## Stack

- **Backend:** Go
- **Frontend:** React

## Ingestion pipeline

1. **Document extraction** with [Docling](https://github.com/docling-project/docling).
2. **Length-aware semantic chunking** using an LLM.
3. **Contextualized chunking**: each chunk is rewritten by an LLM to include context from the chunks before it, for better retrieval.
4. **Graph construction**: entities, relationships, facts and communities are extracted into the canonical knowledge graph.

## Canonical knowledge graph

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

## Agent graph tools

The agent explores the graph through a small set of tools. Each call is traced and shown on the graph.

| Tool | Purpose |
| --- | --- |
| `search_graph(query, node_types, top_k)` | Find candidate nodes for a query |
| `expand_nodes(node_ids, relationship_types, hops, limit)` | Walk outward from nodes along relationships |
| `get_nodes(node_ids)` | Fetch full node content |
| `search_community(query, depth)` | Search community reports at a given level |
| `find_path(source_id, target_id, max_hops)` | Find how two nodes are connected |
| `search_facts(query, entities, date_range)` | Find facts, optionally scoped to entities and dates |

## Node states

| Colour | Meaning |
| --- | --- |
| Grey | In the graph, untouched |
| Blue | Retrieval candidate |
| Purple | Graph traversal passed through it |
| Yellow | Selected by the reranker |
| Green | Actually exposed to the LLM |
| Orange | Referenced or cited in the final answer |

A **bright pulse** marks nodes touched during the current operation.

## Example

> **User:** "How is project Apollo connected to Acme?"

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

On the full graph, the same run looks like this. Nothing in the graph changes; the trace is drawn over it:

```
Before                         During execution

          ○                              ○
    ○─────●────○                   ○─────🟢═══🟡
   /       \                      /       ║
  ○         ○────○               ○        🟢────○
           /                              ║
      ○───○                          ○───🟣
```

## Views

| View | Shows |
| --- | --- |
| **Full graph** | The whole corpus, with the live trace overlaid |
| **Discovered** | Only the subgraph the agent has encountered so far, built up live |

## Use cases

- Debugging retrieval: why did the agent miss that document?
- Understanding agent behaviour: which communities and entities does it lean on?
- Demos and teaching: show how GraphRAG works, visually and live.
