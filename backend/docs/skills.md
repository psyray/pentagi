# Skills and references

PentAGI agents can list, search and read curated skills instead of guessing
from a raw vector store. A skill is a versioned markdown procedure with a short
trigger description, tags and a body. A reference is imported documentation
that has no procedure contract. An engagement is a write-up tied to a target.

The legacy knowledge store (`langchain_pg_embedding`) keeps its current
meaning: flow memory plus agent-generated guides, answers and code. Skills live
in dedicated tables so the legacy Knowledges screen stays unchanged.

## Storage

| Table | Purpose |
|---|---|
| `knowledge_documents` | one row per skill, reference or engagement: slug, title, description, body, status, visibility, tags, metadata |
| `knowledge_chunks` | chunked body, embedding (`vector(1024)`, bge-m3), generated `tsvector` for lexical search |
| `knowledge_files` | attachment metadata; the bytes live in the user resources blob store |
| `knowledge_packs` / `knowledge_pack_items` | optional grouping of documents |
| `knowledge_events` | import, update, delete and future retrieval audit |

Chunks are retrieved with a hybrid query: vector similarity and PostgreSQL
full-text search are fused with Reciprocal Rank Fusion, then one card per
document is returned. The full body is only fetched by `read_skill`.

Statuses: `draft`, `reviewed`, `canonical`, `deprecated`. Deprecated documents
are excluded unless a caller explicitly filters on that status. Visibility:
`private`, `shared`, `admin`. Shared documents are readable by other users only
when their status is `reviewed` or `canonical`.

## Import format

```markdown
---
name: adcs-esc8-ntlm-relay
description: Exploit ADCS ESC8 HTTP enrollment with NTLM relay, with a safe verification and cleanup.
version: 1.2.0
category: active-directory
platform: [windows, ad]
tags: [adcs, esc8, ntlm, relay, certipy]
safety: intrusive
license: CC-BY-4.0
source: https://example.invalid/adcs-esc8
visibility: private
status: draft
language: en
---
# ADCS ESC8, NTLM relay on the HTTP enrollment endpoint

## When to use
...

## Pre-flight checks
...

## Steps
...

## Safe verification
...

## Cleanup
...

## Known limitations
...
```

The frontmatter is optional. Without it, the first `#` heading becomes the
title, the first paragraph becomes the description, the kind defaults to
`reference`, and the slug is derived from the title.

## REST API

All routes require a bearer token or a session, like the rest of the API.
Privileges are `skills.view`, `skills.create`, `skills.search`, `skills.delete`.

### Import a skill

```bash
curl -sS -X POST https://pentagi.easi.local:5443/api/v1/skills/import \
  -H "Authorization: Bearer $PENTAGI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "content": "---\nname: lpd-pjl-fsupload\ndescription: Read a file through PJL FSUPLOAD on an LPD/9100 service.\ntags: [printer, pjl, lpd]\n---\n# PJL FSUPLOAD\n\n## Steps\n...\n",
    "kind": "skill",
    "status": "draft",
    "visibility": "private",
    "tags": ["printing"]
  }'
```

Re-importing the same `(user, kind, slug)` replaces the chunks and increments
the version. The content hash detects an unchanged body; the import still
creates a new version, which keeps the audit trail simple.

### List and read

```bash
curl -sS "https://pentagi.easi.local:5443/api/v1/skills/?kind=skill&tags=adcs" \
  -H "Authorization: Bearer $PENTAGI_API_KEY"

curl -sS "https://pentagi.easi.local:5443/api/v1/skills/adcs-esc8-ntlm-relay" \
  -H "Authorization: Bearer $PENTAGI_API_KEY"
```

### Search

```bash
curl -sS -X POST https://pentagi.easi.local:5443/api/v1/skills/search \
  -H "Authorization: Bearer $PENTAGI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query": "relay NTLM to ADCS HTTP enrollment ESC8", "kind": "skill", "limit": 5}'
```

### Delete

```bash
curl -sS -X DELETE https://pentagi.easi.local:5443/api/v1/skills/adcs-esc8-ntlm-relay \
  -H "Authorization: Bearer $PENTAGI_API_KEY"
```

Deletion is a soft delete of the parent plus a hard delete of the chunks.

## Agent tools

| Tool | Purpose |
|---|---|
| `list_skills` | catalogue, or a search when `query` is supplied |
| `search_skills` | hybrid search, returns cards with an excerpt |
| `read_skill` | full body or one markdown section, plus the attachment list |
| `materialize_skill` | copies the attachments of one skill into `/work/skills/<slug>/` |

They are attached to the assistant, primary, generator, refiner, installer,
coder, pentester, searcher and enricher executors when the shared pgx pool and
the embedding provider are available.

`read_skill` accepts an optional `section` (a markdown heading). Use it when a
reference is longer than the 16 KB tool-result budget. It lists each attachment
with its size and a short sha256, never the bytes.

`materialize_skill(slug, files?, overwrite?)` writes the skill body as
`SKILL.md` and the requested attachments under `/work/skills/<slug>/` inside
the flow container, so the agent runs the provided script instead of rewriting
it. Only the named skill is copied, never the whole catalogue. Files already
present are kept unless `overwrite` is true, so an edited script survives a
second call. The host copy lives in `flow-<id>-data/skills/<slug>/`, is listed
by the flow file browser and is re-synced when the container restarts.

## Limits and current gaps

- Embedding dimension is fixed at 1024 (`bge-m3`). A different model needs a
  migration and a full re-embedding pass.
- Chunk size is 2 000 characters with 200 characters of overlap, heading-aware
  and code-block aware.
- Attachment metadata and the files table exist, and attachments are uploaded,
  downloaded and deleted through the REST and GraphQL routes and the skill page.
- `materialize_skill` copies at most 100 files and 128 MB per call; larger
  skills must be split.
- The legacy 958 knowledge documents are not backfilled into the new tables.
  They remain available through `search_guide`, `search_answer` and
  `search_code`; a backfill can import the curated subset later.
- The catalogue is not injected into the agent prompts yet, and there is no
  deterministic detector: agents reach the skills through the tools only.
- Only markdown is ingested. PDF, DOCX, HTML and ZIP archives need the ingest
  sidecar.
