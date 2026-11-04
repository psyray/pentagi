"""HTTP API of the PentAGI ingestion sidecar.

One endpoint matters: ``POST /extract`` takes a document and returns markdown.
The backend never learns which parser ran, only the markdown, the title and the
warnings.
"""

from __future__ import annotations

import os

from fastapi import FastAPI, File, Form, Header, HTTPException, UploadFile

from app.extract import UnsupportedFormat, extract

MAX_BYTES = int(os.getenv("INGEST_MAX_BYTES") or 64 * 1024 * 1024)
TOKEN = os.getenv("INGEST_TOKEN", "").strip()

app = FastAPI(title="PentAGI ingest", version="1.0.0")


@app.get("/health")
def health() -> dict:
    return {"status": "ok", "version": "1.0.0"}


@app.post("/extract")
async def extract_document(
    file: UploadFile = File(...),
    filename: str | None = Form(default=None),
    x_ingest_token: str | None = Header(default=None),
) -> dict:
    if TOKEN and (x_ingest_token or "") != TOKEN:
        raise HTTPException(status_code=401, detail="invalid ingest token")

    data = await file.read(MAX_BYTES + 1)
    if len(data) > MAX_BYTES:
        raise HTTPException(status_code=413, detail=f"file exceeds {MAX_BYTES} bytes")

    name = (filename or file.filename or "document").strip()

    try:
        result = extract(name, data)
    except UnsupportedFormat as error:
        raise HTTPException(status_code=415, detail=str(error)) from error
    except ValueError as error:
        raise HTTPException(status_code=422, detail=str(error)) from error
    except Exception as error:  # noqa: BLE001 - report the parser failure to the caller
        raise HTTPException(status_code=500, detail=f"extraction failed: {error}") from error

    return result.as_dict()
