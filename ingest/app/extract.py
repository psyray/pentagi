"""Extraction of documents into markdown for the PentAGI skill ingestion.

The sidecar keeps every third-party parser out of the Go backend. The API layer
in ``main.py`` only transports files; the conversion lives here so it can be
tested without HTTP.
"""

from __future__ import annotations

import io
import re
import zipfile
from dataclasses import dataclass, field
from pathlib import Path

MAX_CHARS = 2_000_000
MAX_ZIP_ENTRIES = 200
MAX_PDF_PAGES = 2000

TEXT_SUFFIXES = {".md", ".markdown", ".txt", ".rst", ".log", ".csv", ".tsv", ".json", ".yaml", ".yml"}
HTML_SUFFIXES = {".html", ".htm", ".xhtml"}
PDF_SUFFIXES = {".pdf"}
DOCX_SUFFIXES = {".docx"}
ZIP_SUFFIXES = {".zip"}

SKIPPED_ZIP_PARTS = {"__MACOSX", ".DS_Store", "Thumbs.db"}


class UnsupportedFormat(ValueError):
    """Raised when no extractor handles the file."""


@dataclass
class Extraction:
    markdown: str
    title: str
    source: str
    format: str
    warnings: list[str] = field(default_factory=list)

    def as_dict(self) -> dict:
        return {
            "markdown": self.markdown,
            "title": self.title,
            "source": self.source,
            "format": self.format,
            "chars": len(self.markdown),
            "warnings": self.warnings,
        }


def extract(filename: str, data: bytes, *, max_chars: int = MAX_CHARS) -> Extraction:
    """Convert one uploaded file to markdown."""
    name = (filename or "document").strip()
    if not data:
        raise ValueError("the file is empty")

    suffix = Path(name).suffix.lower()

    if suffix in TEXT_SUFFIXES:
        return _finish(_decode(data), name, "text", max_chars)

    if suffix in HTML_SUFFIXES:
        return _finish(_html(data), name, "html", max_chars)

    if suffix in PDF_SUFFIXES:
        text, warnings = _pdf(data)
        return _finish(text, name, "pdf", max_chars, warnings, data=data)

    if suffix in DOCX_SUFFIXES:
        return _finish(_docx(data), name, "docx", max_chars)

    if suffix in ZIP_SUFFIXES:
        return _zip_extract(name, data, max_chars)

    raise UnsupportedFormat(
        f"unsupported extension {suffix or '(none)'}; "
        "supported: markdown, text, html, pdf, docx, zip"
    )


def _finish(
    text: str,
    name: str,
    fmt: str,
    max_chars: int,
    warnings: list[str] | None = None,
    *,
    data: bytes | None = None,
) -> Extraction:
    warnings = list(warnings or [])
    markdown = text.strip()
    if not markdown:
        warnings.append("no extractable text was found")
    if len(markdown) > max_chars:
        markdown = markdown[:max_chars]
        warnings.append(f"content truncated to {max_chars} characters")

    title = _title(markdown, name, data)
    return Extraction(markdown=markdown, title=title, source=name, format=fmt, warnings=warnings)


def _decode(data: bytes) -> str:
    for encoding in ("utf-8", "utf-16", "latin-1"):
        try:
            return data.decode(encoding)
        except (UnicodeDecodeError, UnicodeError):
            continue
    return data.decode("utf-8", errors="replace")


def _html(data: bytes) -> str:
    raw = _decode(data)
    markdown = ""

    try:
        import trafilatura

        extracted = trafilatura.extract(
            raw,
            include_comments=False,
            include_tables=True,
            favor_recall=True,
            output_format="markdown",
        )
        if extracted and extracted.strip():
            markdown = extracted
    except Exception:
        markdown = ""

    if not markdown:
        markdown = _html_fallback(raw)

    # trafilatura drops a lonely h1 on tiny pages; without a heading the
    # chunker has no section to attach the text to, so promote the page title.
    if not any(line.lstrip().startswith("# ") for line in markdown.splitlines()):
        title = _html_title(raw)
        if title:
            markdown = f"# {title}\n\n{markdown}"

    return markdown


def _html_title(raw: str) -> str:
    import html as html_module

    for pattern in (r"<h1[^>]*>(.*?)</h1>", r"<title[^>]*>(.*?)</title>"):
        match = re.search(pattern, raw, flags=re.IGNORECASE | re.DOTALL)
        if not match:
            continue
        text = re.sub(r"<[^>]+>", " ", match.group(1))
        text = html_module.unescape(re.sub(r"\s+", " ", text)).strip()
        if text:
            return text[:200]
    return ""


def _html_fallback(raw: str) -> str:
    from html.parser import HTMLParser

    class Collector(HTMLParser):
        def __init__(self) -> None:
            super().__init__()
            self.parts: list[str] = []
            self._skip = 0

        def handle_starttag(self, tag, attrs):
            if tag in ("script", "style"):
                self._skip += 1
            elif tag in ("p", "br", "div", "li", "tr"):
                self.parts.append("\n")
            elif tag in {"h1", "h2", "h3", "h4", "h5", "h6"}:
                self.parts.append("\n\n" + "#" * int(tag[1]) + " ")

        def handle_endtag(self, tag):
            if tag in ("script", "style") and self._skip:
                self._skip -= 1
            elif tag in {"h1", "h2", "h3", "h4", "h5", "h6"}:
                self.parts.append("\n")

        def handle_data(self, data):
            if self._skip:
                return
            text = data.strip()
            if text:
                self.parts.append(text + " ")

    parser = Collector()
    parser.feed(raw)
    return "".join(parser.parts)


def _pdf(data: bytes) -> tuple[str, list[str]]:
    import fitz  # pymupdf

    warnings: list[str] = []
    pages: list[str] = []

    with fitz.open(stream=data, filetype="pdf") as doc:
        if doc.is_encrypted:
            raise ValueError("the PDF is encrypted and cannot be read")
        for index, page in enumerate(doc, start=1):
            text = (page.get_text("text") or "").strip()
            if text:
                pages.append(text)
            if index >= MAX_PDF_PAGES:
                warnings.append(f"the PDF has more than {MAX_PDF_PAGES} pages, extraction stopped")
                break

    if not pages:
        warnings.append(
            "the PDF has no text layer (scanned document); OCR is not supported, "
            "attach the file to the skill instead"
        )

    text = "\n\n".join(pages)
    return text, warnings


def _docx(data: bytes) -> str:
    from docx import Document
    from docx.table import Table
    from docx.text.paragraph import Paragraph

    document = Document(io.BytesIO(data))
    parts: list[str] = []

    for block in document.iter_inner_content():
        if isinstance(block, Paragraph):
            text = block.text.strip()
            if not text:
                continue
            style = (block.style.name or "").lower()
            if style.startswith("title"):
                parts.append("# " + text)
            elif style.startswith("heading"):
                parts.append("#" * _heading_level(style) + " " + text)
            else:
                parts.append(text)
        elif isinstance(block, Table):
            rendered = _docx_table(block)
            if rendered:
                parts.append(rendered)

    return "\n\n".join(part for part in parts if part)


def _heading_level(style: str) -> int:
    match = re.search(r"(\d+)", style)
    if match:
        return max(1, min(6, int(match.group(1))))
    return 2


def _docx_table(table) -> str:
    rows = [
        [cell.text.strip().replace("\n", " ") for cell in row.cells]
        for row in table.rows
    ]
    if not rows:
        return ""

    width = max(len(row) for row in rows)
    rows = [row + [""] * (width - len(row)) for row in rows]

    header = "| " + " | ".join(rows[0]) + " |"
    separator = "| " + " | ".join("---" for _ in rows[0]) + " |"
    body = ["| " + " | ".join(row) + " |" for row in rows[1:]]

    return "\n".join([header, separator] + body)


def _zip_extract(name: str, data: bytes, max_chars: int) -> Extraction:
    warnings: list[str] = []
    sections: list[str] = []
    formats: set[str] = set()
    total = 0

    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        entries = [
            info
            for info in archive.infolist()
            if not info.is_dir()
            and not any(part in SKIPPED_ZIP_PARTS for part in Path(info.filename).parts)
        ]
        if len(entries) > MAX_ZIP_ENTRIES:
            warnings.append(f"the archive has more than {MAX_ZIP_ENTRIES} entries, the rest is ignored")
            entries = entries[:MAX_ZIP_ENTRIES]

        for info in entries:
            suffix = Path(info.filename).suffix.lower()
            if suffix not in TEXT_SUFFIXES | HTML_SUFFIXES | PDF_SUFFIXES | DOCX_SUFFIXES:
                warnings.append(f"skipped unsupported entry: {info.filename}")
                continue
            if total >= max_chars:
                warnings.append("character budget reached, the remaining entries are ignored")
                break
            try:
                inner = extract(info.filename, archive.read(info), max_chars=max_chars - total)
            except Exception as error:  # an entry must not fail the whole archive
                warnings.append(f"failed to read {info.filename}: {error}")
                continue
            if inner.warnings:
                warnings.extend(f"{info.filename}: {warning}" for warning in inner.warnings)
            block = f"# {info.filename}\n\n{inner.markdown}".strip()
            sections.append(block)
            formats.add(inner.format)
            total += len(block)

    markdown = "\n\n".join(sections)
    title = _title(markdown, name, None)

    return Extraction(
        markdown=markdown,
        title=title,
        source=name,
        format="zip" if not formats else f"zip({','.join(sorted(formats))})",
        warnings=warnings,
    )


def _title(markdown: str, name: str, data: bytes | None) -> str:
    for line in markdown.splitlines():
        stripped = line.strip()
        if stripped.startswith("# "):
            return stripped[2:].strip()[:200]

    if data is not None and name.lower().endswith(".pdf"):
        try:
            import fitz

            with fitz.open(stream=data, filetype="pdf") as doc:
                meta_title = ((doc.metadata or {}).get("title") or "").strip()
                if meta_title:
                    return meta_title[:200]
        except Exception:
            pass

    stem = Path(name).stem.strip()
    return (stem or "Untitled document")[:200]
