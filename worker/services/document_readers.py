"""Bounded text extraction from office files and scanned documents."""

from __future__ import annotations

import io
import os
import subprocess
import tempfile
import zipfile
from dataclasses import dataclass

import pypdfium2 as pdfium
import pytesseract
from docx import Document
from docx.opc.exceptions import PackageNotFoundError
from docx.table import Table
from PIL import Image, ImageSequence, UnidentifiedImageError


MAX_PAGES = 20
MAX_PIXELS = 12_000_000
MAX_TEXT = 30_000
MAX_DOCX_UNCOMPRESSED = 20_000_000
Image.MAX_IMAGE_PIXELS = MAX_PIXELS

PDF = "application/pdf"
DOC = "application/msword"
DOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
IMAGES = {"image/png", "image/jpeg", "image/tiff"}
READABLE_TYPES = {PDF, DOC, DOCX, "text/plain", *IMAGES}


@dataclass(frozen=True)
class ExtractedText:
    text: str
    method: str
    truncated: bool


def _finish(parts: list[str], method: str) -> ExtractedText:
    text = "\n".join(part.strip() for part in parts if part.strip()).strip()
    if not text:
        raise ValueError("no readable text was found in the document")
    return ExtractedText(text[:MAX_TEXT], method, len(text) > MAX_TEXT)


def _ocr(image: Image.Image) -> str:
    if image.width * image.height > MAX_PIXELS:
        raise ValueError("image exceeds OCR pixel limit")
    try:
        return pytesseract.image_to_string(image, lang="rus+eng", timeout=20)
    except RuntimeError as exc:
        raise ValueError("OCR timed out or failed") from exc


def _pdf(raw: bytes) -> ExtractedText:
    parts = []
    used_ocr = False
    try:
        pdf = pdfium.PdfDocument(raw)
        try:
            if len(pdf) > MAX_PAGES:
                raise ValueError("PDF exceeds page limit")
            for index in range(len(pdf)):
                page = pdf[index]
                try:
                    textpage = page.get_textpage()
                    try:
                        text = textpage.get_text_bounded().strip()
                    finally:
                        textpage.close()
                    if len(text) < 20:
                        width, height = page.get_size()
                        if width * height * 4 > MAX_PIXELS:
                            raise ValueError("PDF page exceeds OCR pixel limit")
                        bitmap = page.render(scale=2)
                        try:
                            recognized = _ocr(bitmap.to_pil())
                        finally:
                            bitmap.close()
                        if len(recognized.strip()) > len(text):
                            text = recognized
                        used_ocr = True
                    parts.append(text)
                finally:
                    page.close()
        finally:
            pdf.close()
    except pdfium.PdfiumError as exc:
        raise ValueError("PDF is damaged, encrypted, or unreadable") from exc
    return _finish(parts, "pdf_text+ocr" if used_ocr else "pdf_text")


def _docx(raw: bytes) -> ExtractedText:
    try:
        with zipfile.ZipFile(io.BytesIO(raw)) as archive:
            entries = archive.infolist()
            if len(entries) > 200 or sum(entry.file_size for entry in entries) > MAX_DOCX_UNCOMPRESSED:
                raise ValueError("DOCX exceeds expanded size limit")
        document = Document(io.BytesIO(raw))
    except (zipfile.BadZipFile, PackageNotFoundError, KeyError) as exc:
        raise ValueError("DOCX is damaged or unreadable") from exc
    parts = []
    for block in document.iter_inner_content():
        if isinstance(block, Table):
            for row in block.rows:
                parts.append("\t".join(cell.text for cell in row.cells))
        else:
            parts.append(block.text)
    return _finish(parts, "docx_text")


def _doc(raw: bytes) -> ExtractedText:
    path = None
    try:
        with tempfile.NamedTemporaryFile(suffix=".doc", delete=False) as file:
            path = file.name
            file.write(raw)
        process = subprocess.run(
            ["antiword", "-m", "UTF-8.txt", path], capture_output=True,
            timeout=20, check=False,
        )
        if process.returncode != 0:
            raise ValueError("DOC is damaged or unreadable")
        return _finish([process.stdout.decode("utf-8", errors="replace")], "doc_text")
    except subprocess.TimeoutExpired as exc:
        raise ValueError("DOC conversion timed out") from exc
    except FileNotFoundError as exc:
        raise ValueError("antiword is not installed") from exc
    finally:
        if path is not None:
            os.unlink(path)


def _image(raw: bytes) -> ExtractedText:
    try:
        with Image.open(io.BytesIO(raw)) as image:
            if getattr(image, "n_frames", 1) > MAX_PAGES:
                raise ValueError("image exceeds page limit")
            parts = []
            for frame in ImageSequence.Iterator(image):
                parts.append(_ocr(frame))
    except (UnidentifiedImageError, OSError, Image.DecompressionBombError) as exc:
        raise ValueError("image is damaged or unreadable") from exc
    return _finish(parts, "image_ocr")


def extract_text(raw: bytes, media_type: str) -> ExtractedText:
    try:
        return _extract_text(raw, media_type)
    except ValueError:
        raise
    except Exception as exc:
        raise ValueError("document could not be read") from exc


def _extract_text(raw: bytes, media_type: str) -> ExtractedText:
    if media_type == "text/plain":
        try:
            text = raw.decode("utf-8-sig")
        except UnicodeDecodeError:
            text = raw.decode("cp1251")
        return _finish([text], "plain_text")
    if media_type == PDF:
        return _pdf(raw)
    if media_type == DOCX:
        return _docx(raw)
    if media_type == DOC:
        return _doc(raw)
    if media_type in IMAGES:
        return _image(raw)
    raise ValueError("unsupported media type")
