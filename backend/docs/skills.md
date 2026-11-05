# Skills, references and engagements

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
| `knowledge_document_versions` | one row per imported version: title, description, body, hash, status, visibility, tags, metadata and actor |
| `knowledge_chunks` | chunked body, embedding (`vector(1024)`, bge-m3), generated `tsvector` for lexical search |
| `knowledge_files` | attachment metadata; the bytes live in the user resources blob store |
| `knowledge_packs` / `knowledge_pack_items` | optional grouping of documents |
| `knowledge_events` | import, update, delete and future retrieval audit |

Chunks are retrieved with a hybrid query: vector similarity and PostgreSQL
full-text search are fused with Reciprocal Rank Fusion, then one card per
document is returned. The lexical side runs twice, once with every query term
(precision) and once with the terms whose document frequency is at or below a
small threshold (recall for identifiers such as ESC8, a CVE id or a tool name).
The full body is only fetched by `read_skill`.

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

An engagement (a write-up of a completed assessment) carries a service fingerprint
on top of the common fields. Every field is optional except `name`:

```yaml
---
name: printer-job-language
platform: HTB
machine: dc01
os: linux
services: [22/ssh, 80/http, 9100/jetdirect]
cves: [CVE-2021-44228]
outcome: user
anonymized: true
tags: [lpd, pjl, fsupload]
---
```

`platform` and `services` accept a scalar or a list. A service is written
`port/service`, in either order (`9100/jetdirect` or `jetdirect/9100`): the
stored form is always lowercase with the port first, so a query and a document
cannot disagree on the spelling. `os` and `outcome` are lowercased, `cves` are
upper-cased and deduplicated, `anonymized` accepts `true`, `yes`, `on`, `1` and
their negatives. The fields land in the document metadata, next to `category`,
`platform` and `license`.

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
| `search_engagements` | ranks past engagements against the target fingerprint |

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

## Catalogue and detection

Two mechanisms make a skill discoverable before the agent thinks of asking for
one.

The **catalogue** is built once per flow, from the flow title and the head of
the initial request, then frozen and cached in the flow provider: every prompt
of the flow receives the same `<available_skills>` block, which preserves the
prompt cache and keeps replays reproducible. It selects at most
`SKILL_CATALOGUE_LIMIT` entries (canonical first, then reviewed, then quality)
and stops at roughly 1500 tokens, replacing the rest with a pointer to
`list_skills`. Drafts are excluded unless `SKILL_CATALOGUE_INCLUDE_DRAFTS` is
set. `SKILL_CATALOGUE_ENABLED=false` removes the block entirely, which is the
control arm of an A/B run. The block is injected into the `primary_agent`,
`pentester`, `coder`, `installer`, `searcher`, `generator` and `refiner`
prompts; the catalogue hash is logged for the audit.

**Detection** is deterministic and costs no model call. `pkg/skills/detectors.yaml`
maps a banner, a port, an error string or a CVE id to a skill slug. When the raw
output of `terminal`, `file` or `web_search` matches a rule, the executor
appends a `<relevant_skills>` block to the tool result, with at most three cards
and each skill surfaced once per flow. A rule whose slug is not imported yet
stays silent, so the file can announce skills before they exist. Set
`SKILL_DETECTORS_PATH` to replace the bundled rules without rebuilding.

## Ingestion of documents (L3.1)

`POST /skills/import/file` takes one multipart document (`file` plus optional
`kind`, `status`, `visibility`, `title`, `tags`, `language`, `packId`), converts
it to markdown with the ingestion sidecar and imports the result through the
same pipeline as the markdown import. The response carries the document and the
extraction report (format, title, warnings).

The sidecar lives in `ingest/` and handles `.md .txt .html .pdf .docx .zip`
(ZIP entries are extracted recursively and concatenated). It is deliberately
kept out of the Go backend: no parser runs in the API process. Enable it with
the overlay:

```bash
docker compose -f docker-compose.yml -f docker-compose-ingest.yml up -d
```

and set `INGEST_URL=http://ingest:8000` plus a shared `INGEST_TOKEN`. With
`INGEST_URL` empty the route answers `503` and nothing else changes. Caps: 64
MiB per upload, 2000 PDF pages, 200 ZIP entries, 2 million characters of
markdown. A scanned PDF with no text layer returns a warning instead of an
error, since OCR is out of scope.

## Packs (L3.2)

A pack is a directory, or a zip of that directory, with a manifest at its root:

```text
web-app/
  pack.yaml
  skills/ssrf-out-of-band/SKILL.md
  skills/ssrf-out-of-band/files/ssrf_probe.py
  references/owasp-wstg/chapter-01.md
  references/owasp-wstg/chapter-02.md
  engagements/htb-example.md
```

```yaml
name: web-app
description: Web application playbooks.
version: 1.0.0
license: private
visibility: private
skills: [ssrf-out-of-band]
references: [owasp-wstg]
engagements: []
```

Rules:

- a skill is a directory with a `SKILL.md`; anything under its `files/` is
  stored as an attachment in the shared resource blob store;
- a reference is one markdown file, or a directory whose files are concatenated
  into one document so retrieval can cite a chapter heading;
- an engagement is one markdown file under `engagements/`, imported as the
  `engagement` kind;
- the three manifest lists act as an allowlist when present: anything not listed
  is left out, which lets a pack ship drafts it does not want imported yet;
- the directory name is the slug, so a reference stays `wstg` even when its
  first heading is `Chapter one`.

Import and export run offline, against the live data directory, so attachments
land in the same blob store the UI reads:

```bash
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --pack /tmp/web-app --status reviewed
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --export-pack web-app --out /tmp/export
```

`--dry-run` prints the documents, their status and their attachment count
without writing. Imports are idempotent: a second run versions the documents and
keeps the same pack. A `pack.yaml` that lists `documents:` instead of the three
lists is the previous flat format, still accepted by the CLI.

## Engagements and fingerprint search (L3.5)

An engagement is a write-up of a completed assessment: a markdown document of
kind `engagement`, imported from a pack under `engagements/` or through the REST
route, with the service fingerprint described above. It is not a procedure, so
it never enters the catalogue; the agent reaches it on purpose.

`search_engagements` ranks the engagements of the scope against the target in
front of the agent. The score rewards the fingerprint rather than the prose:

| Criterion | Weight |
|---|---|
| same `port/service` | 3.0 |
| same port, different service name | 1.5 |
| same service name, different port | 1.5 |
| same `os` | 2.0 |
| same `platform` | 1.0 |
| each shared tag | 0.5 |

Each query service contributes its best candidate, so a document cannot gain
more than one match per queried service, and a repeated service is deduplicated.
A bare token (`80` or `ssh`) matches either part of a fingerprint at the partial
weight, so an agent that only knows the port still gets a hint. A query with at
least one criterion drops the documents that score nothing; an
empty query lists the most recently updated engagements. The response is a
small card per engagement (score, platform, OS, machine, services with the
matched ones starred, outcome) and a `read_skill(slug=...)` hint, never the
write-up.

```bash
# CLI, against the live data directory
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --engagements --services 9100/jetdirect,80/http --os linux
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --engagements --platform HTB --tags lpd
```

An engagement imported before L3.5 has no fingerprint in its metadata: re-importing
its pack versions it and fills `machine`, `os`, `services`, `cves`, `outcome`
and `anonymized` without touching the body. An engagement with no `services` can
still match on `os`, `platform` or tags.

## Review, versions and diff (L3.3)

Each import now appends a row to `knowledge_document_versions` (title,
description, body, hash, status, visibility, tags, actor), so the history starts
at the current state of every document and grows from there. A status change is
recorded in `knowledge_events` as `published`, with the actor, the previous
status and the note.

The lifecycle is enforced: `draft` to `reviewed`, `reviewed` to `canonical` or
back to `draft`, `canonical` to `deprecated` or back to `reviewed`, `deprecated`
to `reviewed` or `draft`. A direct `draft` to `canonical` is refused, a reviewed
document needs a description and a canonical one needs at least one tag.

```bash
# REST
GET  /api/v1/skills/{idOrSlug}/versions
GET  /api/v1/skills/{idOrSlug}/versions/{version}
GET  /api/v1/skills/{idOrSlug}/diff?from=1&to=2
POST /api/v1/skills/{idOrSlug}/publish   {"status":"reviewed","note":"..."}

# CLI, against the live data directory
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --versions adcs-esc8
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --diff adcs-esc8 --from 1 --to 2
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --publish adcs-esc8 --publish-to reviewed --note "relu"
```

The skill page carries a **History** section: every stored version with its
status, size and hash, two selectors to pick the pair to compare, and the
unified patch in a dialog where additions and removals are coloured.

The diff is a unified patch with three lines of context, capped at 500 000
characters per side. A re-import with identical content still bumps the version
counter (it counts imports, not revisions): the diff then reports `identical`
rather than inventing a change.

## Duplicates and merge (L3.4)

The duplicate report pairs the documents of a scope by body similarity, using
the first chunk of each document as its representative vector (pgvector cosine).
It also lists the documents that share an exact content hash.

```bash
GET  /api/v1/skills/duplicates?threshold=0.93

docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --duplicates --threshold 0.93
```

Pairs are grouped into connected components, so a chain of three similar
skills shows up as one group. The default threshold is 0.93; the report never
merges anything, which keeps the 0.93 to 0.98 band for a human decision, as the
plan requires.

A merge folds one or more sources into a target:

```bash
POST /api/v1/skills/merge   {"target":"adcs-esc8","sources":["adcs-esc8-relay"]}

docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --merge adcs-esc8 --merge-from adcs-esc8-relay
```

- the source bodies are appended to the target under a `## Merged from <slug>`
  heading and the target is imported again, so it gains a version it can be diffed against;
- the target description, kind, status, visibility and pack are kept, and the
  tags are unioned;
- the target metadata records `merged_from`, the source metadata records
  `merged_into` and the source status becomes `deprecated`, so it leaves the
  catalogue without losing its history;
- the source attachments move under `files/merged/<source-slug>/` to avoid path
  collisions, and an event `merged` records the operation;
- merging the same source twice is a no-op (`Skipped`), and a source already
  merged into another target is refused.

Metadata pointers are used instead of a new column: the document metadata is
visible in GraphQL and the GIN index already covers it.

## Re-anonymisation (L3.6)

A document imported from a pack, or written before the tools anonymised their
output, may still carry hosts, credentials or a submitted flag. The CLI applies
the same anonymiser the agent tools use, built from the bundled pattern set plus
the secrets of the running configuration:

```bash
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --reanonymize --dry-run
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --reanonymize --only printer-run,adcs-esc8
```

A real run imports a new version per changed document, so every edit stays
diffable and the previous text remains in the history. The slug, kind, status,
visibility, tags and pack are kept, and the metadata the frontmatter cannot
carry is preserved (`merged_from`, `merged_into`, custom keys) with
`anonymized_at` added. A document with nothing left to redact is skipped, so a
second pass is a no-op.

An engagement also has its flag token redacted: a 32 character hexadecimal
string becomes `<REDACTED-FLAG>`, because such a token is only meaningful for
the machine that issued it and no secret pattern knows about it. A reference
keeps a 32 character hex string, which may be a legitimate hash.

`--dry-run` prints what would change without writing. Review it before a real
run: the generic pattern set masks IPv4 addresses, domain names, URLs and
e-mail addresses, so a vendor domain quoted in a reference (`microsoft.com`)
is masked too. Use `--only` to scope a campaign to the documents that matter,
and remember that `--dry-run` still needs a `--user`.

## Evaluation (L4.2)

`skills-import --eval` runs a golden query set against the live store and reports
recall@k, MRR and the empty-search rate, globally and per category. Each case
lists the query, the expected slugs and an optional category; the query is
written the way an agent would phrase it, never as the document title.

```bash
docker cp knowledge-packs/eval/golden.yaml pentagi:/tmp/golden.yaml
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --eval /tmp/golden.yaml --eval-k 3,5 \
  --eval-min-recall 0.9
```

The dataset shipped in `knowledge-packs/eval/golden.yaml` carries 57 cases over
25 categories and targets the 54 documents of the dev corpus (2026-11-05). The
baseline after the rare-term lexical branch is recall@3 0.982, recall@5 0.982,
MRR 0.930 and 0 % empty results; the single miss is `template-detection`, where
two template-injection documents legitimately compete for the same query. Run
it with `--eval-statuses reviewed,canonical` to measure what a non-owner agent
sees in production, where drafts are invisible.

The run only reads, but it costs one embedding call per case. A non-zero exit
means recall at the largest k is below `--eval-min-recall`, which makes the
command usable as a CI gate. `--eval-json` prints the full report for BenchDash.

## Distillation from a flow (L4.1)

A finished flow already contains the procedure that worked: the terminal calls
that produced the evidence. `skills-import --distill-flow` turns one of them into
a draft skill, so the catalogue grows from real engagements instead of manual
write-ups.

```bash
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --distill-flow 195 \
  --distill-redact 'wingdata,WingData,wacky,wingftp' --dry-run
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --distill-flow 195 \
  --distill-redact 'wingdata,WingData,wacky,wingftp' --distill-tags paper-printer
```

The evidence decides what is kept. A shell as a new user, a root shell or a read
flag is a proof point; a hash alone is not, because a hash in a transcript is
usually a file name or a tool banner. Around each proof point the builder keeps
a window of two commands, so the step that produced the evidence keeps its
context. When no proof point is found, the last commands of the subtasks that
closed with `objective-achieved-evidence` are used; without any of that the
first 40 commands are kept and the draft says so in its limitations.

Every command is redacted before it is written: the literals passed to
`--distill-redact`, the generic anonymiser, and a 32 character hexadecimal token
inside a flag-proof command becomes `<REDACTED-FLAG>`. The draft is imported as
`kind=skill`, `status=draft`, `visibility=private`, `source_kind=distilled`,
`source_uri=pentagi://flow/<id>`, with `distill_flow_id`, `distill_commands`,
`distill_evidence` and `distill_generated_at` in its metadata. A distilled draft
is never canonical: a human reviews it with `--publish` before it reaches another
agent.

The run refuses to import when a near duplicate already exists (same similarity
threshold as `--duplicates`); `--distill-force` overrides the refusal. `--dry-run`
prints the markdown it would import and skips the duplicate check, because it
needs no embedding.

## Retrieval usage (L4.4)

The skill tools record two events in `knowledge_events`: `searched`, with the hit
slugs, their rank and their score, and `read`, tied to the document id and
carrying the slug. The raw query is not stored, only a 16 character hash, so the
usage table never duplicates target data. A metrics write is best effort: a
failure is logged and the tool call succeeds.

```bash
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --stats --stats-days 30
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --stats --stats-json
```

The report shows the number of searches, the empty-search rate, how many skills
were suggested, how many were read and which share of reads followed a
suggestion. It then lists every document visible to the user with its suggestion
count, read count and last seen date, and repeats the documents that were never
suggested nor read in the window. Success per skill (flags, shells) is not
computed here: that needs the benchmark harness, which reads the same events.

`--stats` needs no embedding client, so it works while the embedding service is
unreachable. The `searched` and `read` values are also the audit trail of what an
agent was shown: a suggestion that is never read is a ranking or writing problem,
not a coverage problem.

## Review and deprecation (L4.5)

A curated document that nobody suggests or reads slowly becomes noise in the
catalogue. `--stale` lists the candidates: reviewed or canonical, no suggestion,
no read for the window, and unchanged for as long as the window.

```bash
docker exec -w /opt/pentagi pentagi /opt/pentagi/bin/skills-import \
  --user admin@pentagi.com --stale --stats-days 90
```

Deprecation stays manual: `--publish <slug> --publish-to deprecated --note "..."`
keeps the document readable for its owner and removes it from what other agents
see. Never delete a document that taught something: deprecate it, so its history
and its versions remain diffable. A document that is stale but still cited by an
eval case is either a corpus gap or a ranking gap, and the eval run tells which
one before anything is retired.

## Pack scoping and A/B runs (L4.3)

A flow can be restricted to explicit packs. The selection is stored on the flow
(`flows.skill_pack_ids`, empty by default) and applied everywhere the flow reads
skills: the catalogue injected into the prompts, `list_skills`, `search_skills`,
`read_skill`, the engagement search, the automatic surfacing and the
`<relevant_skills>` block of the legacy knowledge tools. A `read_skill` of a
document outside the selection answers "not found", so an agent cannot escape
the scope by replaying a slug it read elsewhere.

```graphql
mutation {
  createFlow(
    modelProvider: "Optimized cloud v2"
    input: "Assess 10.129.x.x, credentials in the brief"
    skillPackIds: ["a3f1c2d4-0000-4000-8000-000000000000"]
  ) { id }
}
```

The REST route accepts the same selection as `skill_pack_ids` in its create
payload. An absent or empty list means "every visible pack", which keeps every
existing flow unchanged and is the control arm of a comparison.

The A/B protocol is therefore: same box, same provider, same prompts, two flows;
the control passes no `skillPackIds`, the treatment passes the pack under test.
The log line `skill catalogue frozen for the flow` carries the `packs` field,
so a run can be checked afterwards, and the retrieval events are recorded in
both arms: `--stats` then shows whether the pack was suggested and read at all.

Two properties matter for reading the result. The selection is fixed when the
flow is created, because the catalogue is frozen for the flow's lifetime; and it
is fail closed, so an unknown pack id yields an empty catalogue instead of the
whole corpus. A document imported without a pack is invisible to a scoped flow,
which is the intended way to keep an untrusted pack out of an engagement:
curate the pack explicitly rather than rely on exclusion.

## Limits and current gaps

- Embedding dimension is fixed at 1024 (`bge-m3`). A different model needs a
  migration and a full re-embedding pass.
- Chunk size is 2 000 characters with 200 characters of overlap, heading-aware
  and code-block aware.
- Attachment metadata and the files table exist, and attachments are uploaded,
  downloaded and deleted through the REST and GraphQL routes and the skill page.
- `materialize_skill` copies at most 100 files and 128 MB per call; larger
  skills must be split.
- A pack import is not transactional across documents: a failure in the middle
  leaves the documents imported before it. Re-running the same pack converges.
- A merge is not reversible from the UI: undoing it means restoring the source
  by hand, though its content stays in the version history.
- `search_engagements` reads metadata only: an engagement imported before L3.5
  stays invisible to the fingerprint search until its pack is re-imported. The
  score is a heuristic, not a ranking trained on outcomes.
- Re-anonymisation masks with the generic pattern set: it can over-redact a
  vendor domain in a reference. The dry run is the review gate, and there is no
  per-pattern allow list yet.
- Automatic merge at import (similarity 0.98 and above) is not implemented:
  every merge is explicit.
- The history starts at the moment the versions table was created: earlier
  revisions of a document are not recoverable.
- The review lifecycle is enforced by the publish action; the metadata editor
  still lets an owner set any status directly, which is the escape hatch for an
  operator.
- The legacy 958 knowledge documents are not backfilled into the new tables.
  They remain available through `search_guide`, `search_answer` and
  `search_code`; a backfill can import the curated subset later.
- Images, scanned PDFs (no text layer) and audio are not converted: OCR is out
  of scope, attach those files to a skill instead.
- The ingestion sidecar is not packaged in the installer yet: it is built from
  the source tree through `docker-compose-ingest.yml`.
- The golden set targets one corpus snapshot: a document removed or renamed must
  be updated in `knowledge-packs/eval/golden.yaml`, otherwise the case counts as
  a miss until someone fixes it.
- Distillation only keeps what the flow logged as a terminal call: work done
  inside a script that was never printed yields a thin draft. The draft is also
  phrased for the target of that flow, which is why it stays private until a
  human generalises it.
- Usage events come from the agent tools only. A read through REST, GraphQL or the
  web UI is not counted yet, and the automatic surfacing is not either, so the
  read-after-suggestion rate is a lower bound.
- `--stats` counts one suggestion per hit, so a document that stays in the top 5
  of twenty searches shows twenty suggestions whatever its rank was.
- `updated_at` is touched by any write, including a metadata edit, so `--stale`
  treats a recently edited document as fresh even if its body did not change.
- A pack selection cannot be changed on a running flow: the catalogue is frozen
  when the flow starts, and a different selection needs a new flow.
- The pack selection restricts reads only. It does not restrict writes, so an
  agent can still import a skill into the store from a scoped flow, and that
  document is visible to the next unscoped flow.
