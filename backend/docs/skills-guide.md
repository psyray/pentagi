# Using skills: a practical guide

This guide is for the people who write, curate and use skills with PentAGI. It
answers three questions: what the agent actually sees, how to write a skill it
will find, and how to keep the library useful over time. It contains no
instance-specific configuration. The developer and operator reference, with the
tables, the SQL, the storage layout and the API internals, is
[`skills.md`](skills.md).

## 1. The mental model

PentAGI stores three kinds of document in one library:

| Kind | What it is | How the agent uses it |
|---|---|---|
| `skill` | A procedure: when to use it, checks, steps, verification, cleanup | Runs the steps, or copies its scripts and runs them |
| `reference` | Documentation: a methodology, a vendor guide, a checklist | Reads a section before acting |
| `engagement` | A write-up of a past assessment, with a service fingerprint | Compares a target with past targets to reuse what worked |

Two fields decide who sees a document and what the agent is allowed to use:

- **status**: `draft`, `reviewed`, `canonical`, `deprecated`;
- **visibility**: `private`, `shared`, `admin`.

The rule that matters for day-to-day use:

| Situation | Agent catalogue | Agent tools | Other users |
|---|---|---|---|
| `draft` + `private` | no | yes, for its owner's flows | no |
| `reviewed` or `canonical` + `private` | yes | yes | no |
| `reviewed` or `canonical` + `shared` | yes | yes | yes |
| `deprecated` | no | only if explicitly asked | no |

So a draft is a safe place to write: it is invisible to the prompts, and only
its owner can read it with `read_skill`. Promotion to `reviewed` is what makes a
document eligible for the catalogue of its owner's flows; `shared` is what
extends it to the other users of the instance.

Skills are not the flow memory. Flow memory (`search_in_memory`) is what this
engagement learned; the legacy knowledge tools read what previous flows wrote;
the skill library is what a human curated for reuse. An agent that needs a
procedure should ask the skill library, not the memory.

## 2. How an agent finds a skill

There are four paths, and they fire in this order.

**1. The catalogue.** When a flow starts, PentAGI builds a compact list of the
relevant documents and injects it into the system prompts of the primary agent,
the pentester, the coder, the installer, the searcher, the generator and the
refiner. The list is built from the flow title and the head of the initial
request, is capped at 40 entries and roughly 1,500 tokens, and excludes drafts.
It is built once and frozen for the whole flow, so every step sees the same
list. If the list is longer than the budget, the tail becomes a pointer to
`list_skills`.

**2. The tools.** The agent can always browse and search on its own:

| Tool | What it returns |
|---|---|
| `list_skills` | The library listing with filters (`kind`, `status`, `tags`), or a search when `query` is given |
| `search_skills` | Ranked cards: title, description, tags, the matched section, a short excerpt, five by default |
| `read_skill` | The full body, or one section, plus the attachment list |
| `materialize_skill` | Copies the skill body and its scripts into the flow container |

**3. The automatic hint.** When the raw output of `terminal`, `file` or
`web_search` matches a detector rule (an SSH banner, port 9100, an LDAP error, a
CVE id), the tool result gains a `<relevant_skills>` block with at most three
cards. Each skill is surfaced at most once per flow. This is how a skill
announces itself when the target looks like its trigger.

**4. The legacy tools.** `search_guide`, `search_answer` and `search_code`
append a short `<relevant_skills>` block to their result, so an agent that still
uses the old tool names also sees the curated library.

Two consequences for whoever writes skills:

- the agent decides. The catalogue is a hint, and reading is an explicit action.
  A skill that is never read was either not visible or not convincing at the
  card stage;
- retrieval is search. The description, the tags and the "When to use" lines are
  matched against the words an agent types, not against a human table of
  contents.

## 3. Writing a skill that gets found

### Frontmatter

Only the fields below matter for retrieval and for the review workflow.

```markdown
---
name: adcs-esc8-ntlm-relay
description: Relay NTLM to the ADCS HTTP enrollment endpoint (ESC8) and read the issued certificate.
category: active-directory
platform: [windows, ad]
tags: [adcs, esc8, ntlm, relay, certipy, petipotam]
safety: intrusive
source: https://example.invalid/adcs-esc8
visibility: private
status: draft
---
```

| Field | Advice |
|---|---|
| `name` | The slug. It is the identifier the agent will see and quote, so keep it short and descriptive (`adcs-esc8-ntlm-relay`, not `doc-2026-11-05`) |
| `description` | One line, written as the problem it solves, with the words an agent would search for (protocol, product, error, port, CVE) |
| `tags` | The same vocabulary, plus the tool names. Five to ten tags, no sentences |
| `category`, `platform` | Stored in the metadata and added to the tags, so they also serve filtering. The engagement fingerprint uses `platform`, `os`, `services` |
| `safety` | Free text, by convention `safe`, `intrusive` or `destructive`. It tells the reader what to warn the client about |
| `visibility` / `status` | Defaults to `private` and `draft`. Leave them unless you are importing a curated pack |

Never put the client name, a target hostname, a real credential or a flag in a
shared document. Use `example.invalid` in examples.

### Body

The body is a procedure, and the agent reads it to act. The shape below is the
one the prompts and the reviewers expect, because it answers the questions an
agent asks itself: should I use this, what do I check first, what do I run, how
do I know it worked, how do I leave the system as I found it.

```markdown
# ADCS ESC8, NTLM relay on the HTTP enrollment endpoint

## When to use
A certificate authority exposes the HTTP enrollment endpoint and SMB signing is
not required. Symptoms: `certsrv` on 80/443, `ESC8` in a scan note.

## Pre-flight checks
A relay listener can bind 445, the DC is reachable, and the account can enroll.

## Steps
1. Start the relay against the enrollment URL.
2. Coerce an authentication from a machine account.
3. Request the certificate and note its template.

## Safe verification
The issued certificate carries the expected template and principal. Do not
enumerate further.

## Cleanup
Stop the listener, revoke the test certificate, remove the temporary files.

## Known limitations
Requires an unauthenticated coercion path; fails when EPA is enforced.
```

Practical rules:

- **Repeat the searchable words.** "ESC8", "certsrv", "NTLM relay" belong in the
  description, in the tags and in "When to use". This is what makes the hybrid
  search return the document for a natural query;
- **Write for a reader that has tools but no context.** Name the commands, the
  paths and the expected outputs. Say what "success" looks like;
- **Keep a verification and a cleanup section.** A skill that cannot prove it
  worked, or that leaves a listener running, is not promotable;
- **Stay under about 2,000 characters per section.** Longer references are
  chunked and read section by section, which works, but a wall of text is read
  once and abandoned;
- **Attach scripts instead of pasting them.** Put the file under `files/`, name
  it in the steps, and tell the reader to call `materialize_skill` so it runs the
  tested version rather than retyping it.

### Engagements

An engagement is the same markdown plus a fingerprint, and it is what makes a
past target reusable. Fill `machine`, `os`, `services` (as `port/service`),
`cves`, `outcome` (for example `user`, `root`, `domain`, `forest`) and
`anonymized: true` once the write-up carries no client data. The fingerprint
search compares a new target with the stored services, the port, the operating
system, the platform and the tags, and returns the closest past assessments:
that is how an agent starts an engagement with what already worked elsewhere. An
engagement without a fingerprint stays readable, but the fingerprint search is
what surfaces it at the right moment.

## 4. Getting skills into the library

Three ways, all equivalent, all idempotent on the slug.

**The Skills page.** It lists, searches and filters the library, which is also
the place to check what an agent is supposed to see. Create a document by pasting
markdown, import a file (PDF, DOCX, HTML, ZIP, markdown) which is converted to
markdown, edit the metadata (status, visibility, tags, description) from the
document page, add and remove attachments. The document page also shows the
version history and the diff between two versions.

**A pack.** A pack is a directory with a `pack.yaml` and three folders, so a
curated set can be versioned, reviewed and shipped as one unit:

```
web-app/
  pack.yaml
  skills/
    ssrf-out-of-band/SKILL.md
    ssrf-out-of-band/files/ssrf_probe.py
  references/
    owasp-wstg/chapter-01.md
    owasp-wstg/chapter-02.md
  engagements/
    htb-example.md
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

Each list holds slugs, and the directory or file name is the slug. A skill is a
directory with a `SKILL.md` and its `files/` become attachments; a reference is
one markdown file or a directory whose chapters are concatenated; an engagement
is one markdown file. The three lists act as an allowlist: anything not listed
is left out, which lets you ship a work in progress that nobody imports yet. An
import with `--status reviewed` promotes the whole pack in one step, which is how
a curated set moves from the review branch to the catalogue.

Importing a pack twice does not duplicate anything: each listed document is
re-imported as a new version, even when its body is unchanged, so the version
number grows and the diff stays empty.

**The API and the CLI.** For automation, use the REST routes (`POST
/skills/import`, `/skills/import/file`, `/skills/search`, `/skills/{id}/publish`)
or the `skills-import` binary shipped in the application image, which supports
the same operations plus the offline reports (`--eval`, `--stats`, `--stale`,
`--distill-flow`, `--duplicates`). The CLI flag list is in the quick reference
at the end of this guide.

Whatever the entry point, re-importing the same `(user, kind, slug)` creates a
new version of the same document. Create a new slug only when the procedure
itself is different.

## 5. Curating: the review workflow

`draft` is the workspace, `reviewed` is a document you trust enough to show to
other agents and other users, `canonical` is the trusted procedure of the
library, and `deprecated` is the archive. Promotion is always a deliberate
action, and these are the moves the publish action allows:

| From | To | Condition |
|---|---|---|
| `draft` | `reviewed` | A description is required |
| `reviewed` | `canonical` | At least one tag is required |
| `canonical` | `reviewed` | Downgrade after a doubt |
| `canonical` | `deprecated` | Retire a trusted skill |
| `deprecated` | `reviewed` or `draft` | Bring a retired skill back |
| `draft` | `canonical` | Refused: a draft is reviewed first |

What to check before promoting:

1. The procedure is reproducible as written, and the verification proves it
   worked.
2. The document carries no client data, no credential, no flag, no target name.
3. The description contains the words an agent would use to look for it.
4. The cleanup is explicit, or the "Known limitations" section says why not.

**Versions.** Every import is a version. Compare two versions from the
document's History section or with the CLI (`--versions`, `--diff`). Use it to
see what a colleague changed before promoting.

**Duplicates and merge.** The duplicate report lists pairs whose first chunk is
at least 93% similar, and the exact duplicates by content hash. Merging appends
the source bodies to the target under a `## Merged from <slug>` heading,
re-imports the target as a new version, and marks each source `deprecated` with
`merged_into` in its metadata. The sources keep their history. A merge is
explicit: nothing is merged automatically at import.

**Re-anonymisation.** A document written before the anonymiser existed may still
carry hosts, credentials or a submitted flag. The CLI applies the same patterns
the agent tools use, as a new version. Always run the dry run first and read the
list: the generic pattern set masks domains too, so a vendor domain quoted in a
reference shows up as a change.

**Deprecation.** Retire, do not delete. A deprecated document leaves the
catalogue and the default search results, stays readable for its owner, and
keeps its history. Deleting removes the chunks and keeps an orphaned audit
trail, which is the wrong trade-off for a document that once taught something.

**Sharing.** `private` is the default and the right default. `shared` only
matters if the status is `reviewed` or `canonical`, because a shared draft stays
invisible to other users. `admin` is for the operator who maintains a common
library.

## 6. Using skills in an engagement

For an ordinary flow there is nothing to configure: the catalogue is built and
the tools are attached. Two situations deserve a deliberate choice.

**Scoping a flow to packs.** When the library mixes corpora (several clients,
several labs, a public pack and a private one), restrict the flow to a pack so
the agent cannot read outside its scope. In GraphQL:

```graphql
mutation {
  createFlow(
    modelProvider: "<provider>"
    input: "<brief>"
    skillPackIds: ["<pack-uuid>"]
  ) { id }
}
```

The REST create route accepts the same selection as `skill_pack_ids`. An absent
or empty list means every visible pack. The selection covers the catalogue, the
four tools, the engagement search, the automatic hint and the legacy dual-read
block, and a `read_skill` outside the selection answers "not found". It is fixed
when the flow is created, and it fails closed: an unknown pack id yields an
empty catalogue rather than the whole library.

**Running an A/B comparison.** Same box, same provider, same prompts, two flows:
the control passes no pack, the treatment passes the pack under test. Check the
catalogue log line of each flow (it carries the pack list and the number of
entries), and read `--stats` afterwards to see whether the pack was suggested
and read at all.

**Checking that the agent sees the library.** Ask it to list what it can see, or
read the tool result in the flow logs. A flow whose `list_skills` returns nothing
has no visible document for its scope: check the statuses (drafts are excluded
from the catalogue but still listable), the pack selection, and whether the
embedding service was reachable when the flow started.

## 7. Measuring and improving the library

**Usage.** The tools record two events: `searched` (the suggested slugs with
their rank and score) and `read` (the document the agent opened). The query
itself is never stored, only a short hash. The report gives five numbers:
searches, empty searches, suggestions, reads, and the share of reads that
followed a suggestion, then the per-document detail and the documents that were
never suggested nor read. Read it with:

```bash
skills-import --user <operator> --stats --stats-days 30
skills-import --user <operator> --stats --stats-json
```

A document that is suggested but never read is a ranking or a wording problem.
A document that is never suggested is either invisible (status, pack, scope) or
absent from the vocabulary the agents use.

**Relevance.** A golden query set measures the search itself: recall@k, MRR and
the empty rate, globally and per category, with an optional gate for CI. Write
the queries the way an agent phrases them, never as the document title, and add
one case whenever you add a skill.

```bash
skills-import --user <operator> --eval golden.yaml --eval-k 3,5 \
  --eval-min-recall 0.9
skills-import --user <operator> --eval golden.yaml --eval-statuses reviewed,canonical
```

The second form restricts the corpus to what the catalogue can show, so it
measures the retrieval a flow actually starts from rather than the whole
library.

**Staleness.** `--stale --stats-days 90` lists the reviewed or canonical
documents that nobody suggested or read and that were not edited for the window.
Deprecate them, or fix the description that made them unfindable.

**Distillation.** A finished flow already contains the procedure that worked.
`--distill-flow` turns it into a private draft: it keeps a two-command window
around each proof (a shell as a user, a root shell, a read flag), falls back on
the subtasks that closed as objective achieved, and caps the result at 40
commands. The draft is redacted (your own literals, the anonymiser, the flag
token) and imported as `draft` / `private` with `source_kind=distilled` and the
flow id in its metadata. Never publish it as is: rewrite the context, remove
what is specific to the target, and only then promote it.

**The loop.** Run an engagement, distill what worked, review the draft against
the checklist of section 5, promote it, add a golden query for it, then watch the
usage and prune what nobody reads. That loop is the whole point of the library:
it grows from engagements, not from documentation projects.

## 8. Troubleshooting

| Symptom | Likely cause | What to do |
|---|---|---|
| The agent never mentions a skill | It is a `draft`, and drafts are excluded from the catalogue | Promote it to `reviewed`, or ask it to `list_skills` explicitly to confirm it is there |
| The catalogue is empty | No reviewed or canonical document, or a pack selection that matches nothing, or the embedding service was down at flow start | Check the statuses, the flow's pack ids, and the catalogue log line |
| The agent reads the wrong skill | Two documents answer the same words | Sharpen the descriptions, add distinctive tags, merge the duplicates |
| The agent finds nothing with its own query | The description uses the human vocabulary, not the agent's | Put the product, the port, the error string and the CVE in the description and the "When to use" section |
| Another user cannot see a skill | `private`, or `shared` but still `draft` | Set `shared` and promote to `reviewed` |
| A reference is truncated in the answer | The body is longer than a tool result | Ask for one section, or split the reference by chapter |
| A script is missing in the container | It was never materialised | Call `materialize_skill`, which copies it under `/work/skills/<slug>/` |
| An A/B run shows no difference | The pack contains only drafts, or the catalogue is disabled for both arms | Promote the pack, or check the catalogue setting |
| The agent wrote a skill nobody asked for | Agents can import their own drafts | Review the library regularly, and deprecate what is noise |

## 9. Safety and hygiene

- **Private first.** A document becomes visible to other users only when you
  decide it, with the status and the visibility together.
- **No engagement data in a shared skill.** Hosts, credentials, flags, client
  names and screenshots belong to the engagement, not to a reusable procedure.
  Anonymise before promoting, and run the re-anonymisation dry run on anything
  imported from a pack you did not write.
- **Say what it breaks.** `safety: intrusive` or `destructive`, and a cleanup
  section, tell the next user what the procedure costs.
- **Do not trust a promotion blindly.** A `canonical` document is shown to every
  flow of its owner, and to the flows of other users when it is `shared`: read
  the diff since the last version before you promote or after someone edits it.
- **Scope client work.** When the library holds several client corpora, create
  the flow with an explicit pack selection. Note that the selection restricts
  reads only: an agent can still import a new draft into the library, so review
  what the flows of a client left behind.

## 10. Quick reference

### Frontmatter fields

| Field | Meaning |
|---|---|
| `name` | Slug, the identifier the agent sees |
| `description` | The searchable one-liner |
| `tags` | Searchable keywords and tool names |
| `category`, `platform`, `license`, `source` | Metadata and provenance. `category` and `platform` are also appended to the tags |
| `safety` | Free text, by convention `safe`, `intrusive`, `destructive` |
| `visibility` | `private`, `shared`, `admin` |
| `status` | `draft`, `reviewed`, `canonical`, `deprecated` |
| `machine`, `os`, `services`, `cves`, `outcome`, `anonymized` | Engagement fingerprint only |

### Agent tools

| Tool | Key arguments |
|---|---|
| `list_skills` | `kind`, `status`, `tags`, `query`, `limit` (20 by default) |
| `search_skills` | `query`, `kind`, `status`, `tags`, `limit` (5 by default) |
| `read_skill` | `slug` or id, optional `section` |
| `materialize_skill` | `slug`, optional `files`, `overwrite` |
| `search_engagements` | `services` (`9100/jetdirect`), `os`, `platform`, `tags`, `limit` |

### CLI

| Command | Purpose |
|---|---|
| `--pack <dir> [--dry-run] [--only slug1,slug2]` | Import a pack |
| `--export-pack <name> --out <dir>` | Export a pack back to files |
| `--search "<query>"` | Check retrieval from a shell |
| `--versions <slug>` / `--diff <slug> --from 1 --to 2` | History |
| `--publish <slug> --publish-to reviewed --note "..."` | Promote or deprecate |
| `--duplicates [--threshold 0.93]` / `--merge <slug> --merge-from a,b` | Duplicates and merge |
| `--reanonymize [--dry-run] [--only slug1,slug2]` | Redact a document |
| `--engagements --services 22/ssh,80/http` | Rank past engagements |
| `--eval <file> --eval-k 3,5 [--eval-statuses …] [--eval-min-recall 0.9]` | Golden set |
| `--stats [--stats-days 30] [--stats-json]` | Usage report |
| `--stale [--stats-days 90]` | Review candidates |
| `--distill-flow <id> [--distill-redact a,b] [--distill-tags …] [--dry-run]` | Draft from a flow |
| `--delete <slug>` | Soft delete |

### Settings that change what the agent sees

| Setting | Default | Effect |
|---|---|---|
| `SKILL_CATALOGUE_ENABLED` | `true` | `false` removes the catalogue block, which is the control arm of a comparison |
| `SKILL_CATALOGUE_LIMIT` | `40` | Maximum entries in the catalogue |
| `SKILL_CATALOGUE_INCLUDE_DRAFTS` | `false` | `true` shows drafts too, which is for testing a library, not for production |
| `SKILL_DETECTORS_PATH` | bundled | Replaces the detection rules without rebuilding |

For the storage layout, the REST and GraphQL internals, the SQL and the
migration history, read [`skills.md`](skills.md).
