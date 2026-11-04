# Ingestion sidecar

Turns uploaded documents into markdown for the PentAGI skill import. Kept out of
the Go backend, which stays free of PDF, DOCX and HTML parsers.

## Contract

```
GET  /health
POST /extract     multipart: file, optional filename
                  header: X-Ingest-Token when INGEST_TOKEN is set
```

Response:

```json
{
  "markdown": "# Title\n\n...",
  "title": "Title",
  "source": "document.pdf",
  "format": "pdf",
  "chars": 12345,
  "warnings": []
}
```

Status codes: `401` bad token, `413` file too large, `415` unsupported
extension, `422` unreadable document, `500` parser failure.

## Formats

| Extension | Parser | Notes |
|---|---|---|
| `.md .markdown .txt .rst .log .csv .tsv .json .yaml .yml` | passthrough | UTF-8, UTF-16, latin-1 |
| `.html .htm .xhtml` | trafilatura, HTML parser fallback | tables kept, headings kept |
| `.pdf` | pymupdf | no OCR: a scanned PDF returns a warning, not an error |
| `.docx` | python-docx | headings mapped to `#`, tables rendered as markdown |
| `.zip` | recursive | supported entries concatenated under `# <entry>`, unsupported ones reported |

Caps: 64 MiB per upload (`INGEST_MAX_BYTES`), 2000 PDF pages, 200 ZIP entries,
2 000 000 characters of markdown.

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `INGEST_TOKEN` | empty | shared secret, required in `X-Ingest-Token` when set |
| `INGEST_MAX_BYTES` | `67108864` | upload cap |

## Build and run

```bash
docker build -t vxcontrol/pentagi-ingest:local .
docker run --rm -p 8000:8000 -e INGEST_TOKEN=change-me vxcontrol/pentagi-ingest:local
```

With the PentAGI stack, use the overlay at the repository root:

```bash
docker compose -f docker-compose.yml -f docker-compose-ingest.yml up -d
```

and set in the backend environment:

```
INGEST_URL=http://ingest:8000
INGEST_TOKEN=change-me
```

Leaving `INGEST_URL` empty disables the endpoint: `POST /skills/import/file`
then answers `503`, and the markdown import keeps working.

## Tests

```bash
cd ingest
pip install -r requirements.txt -r requirements-dev.txt
python -m pytest -q
```
