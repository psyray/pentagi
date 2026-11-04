import io
import zipfile

import pytest

from app.extract import UnsupportedFormat, extract


def test_markdown_passthrough_keeps_the_heading_as_title():
    data = b"---\ntags: [ad]\n---\n# Kerberoasting\n\nRequest a service ticket and crack it.\n"

    result = extract("kerberoast.md", data)

    assert result.format == "text"
    assert result.title == "Kerberoasting"
    assert "Request a service ticket" in result.markdown
    assert result.warnings == []


def test_html_is_converted_to_markdown_with_its_title():
    data = b"<html><head><title>ESC8</title></head><body><h1>ESC8 relay</h1><p>Relay NTLM to the CA.</p></body></html>"

    result = extract("esc8.html", data)

    assert result.format == "html"
    assert "ESC8 relay" in result.markdown
    assert "Relay NTLM to the CA." in result.markdown
    assert result.title == "ESC8 relay"


def test_docx_keeps_headings_tables_and_paragraphs():
    from docx import Document

    document = Document()
    document.add_heading("LPD PJL", level=1)
    document.add_paragraph("Send a PJL FSUPLOAD request.")
    table = document.add_table(rows=2, cols=2)
    table.cell(0, 0).text = "port"
    table.cell(0, 1).text = "service"
    table.cell(1, 0).text = "9100"
    table.cell(1, 1).text = "jetdirect"

    buffer = io.BytesIO()
    document.save(buffer)

    result = extract("printer.docx", buffer.getvalue())

    assert result.format == "docx"
    assert result.title == "LPD PJL"
    assert "| port | service |" in result.markdown
    assert "| 9100 | jetdirect |" in result.markdown
    assert "Send a PJL FSUPLOAD request." in result.markdown


def test_pdf_with_a_text_layer_is_extracted():
    import fitz

    document = fitz.open()
    page = document.new_page()
    page.insert_text((72, 72), "Paperwork user flag lives in the printer file.")
    data = document.tobytes()
    document.close()

    result = extract("paperwork.pdf", data)

    assert result.format == "pdf"
    assert "Paperwork user flag" in result.markdown
    assert result.title == "paperwork"


def test_zip_concatenates_supported_entries_and_reports_the_rest():
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w") as archive:
        archive.writestr("notes/adcs.md", "# ADCS\n\nRelay to the CA.\n")
        archive.writestr("notes/lpd.txt", "PJL FSUPLOAD\n")
        archive.writestr("tools/relay.py", "print('binary-ish')\n")
        archive.writestr("__MACOSX/notes/._adcs.md", "junk")

    result = extract("pack.zip", buffer.getvalue())

    assert result.format.startswith("zip")
    assert "# notes/adcs.md" in result.markdown
    assert "# notes/lpd.txt" in result.markdown
    assert "relay.py" not in result.markdown
    assert any("skipped unsupported entry" in warning for warning in result.warnings)


def test_unsupported_extension_is_refused():
    with pytest.raises(UnsupportedFormat):
        extract("binary.bin", b"\x00\x01")


def test_empty_file_is_refused():
    with pytest.raises(ValueError):
        extract("empty.md", b"")


def test_truncation_is_reported():
    result = extract("long.txt", b"a" * 500, max_chars=100)

    assert len(result.markdown) == 100
    assert any("truncated" in warning for warning in result.warnings)
