"""Write what PyMuPDF reads from every PDF in a folder, for TestAgainstPyMuPDF.

Usage: python scripts/pymupdf_golden.py <folder> [dpi,dpi,…]
(writes <folder>/pymupdf.jsonl)

One JSON line per PDF: its page count, or the error PyMuPDF raised opening
it, and per page the plain text (page.get_text()), the sorted text
(page.get_text("text", sort=True), null where PyMuPDF raises), whether the
page lists an image, its words' texts (page.get_text("words")), its box
(page.rect) and the boxes of the images it draws (page.get_image_info()).
With DPIs, also each page drawn at each (page.get_pixmap(dpi=…)): its size,
and the SHA-256 of its PNG (tobytes("png")) and of its RGB samples.

Also per page: its blocks (get_text("blocks")), its rawdict
(get_text("rawdict"), each image's and mask's bytes as their SHA-256) and its
drawings (get_drawings(), and get_drawings(extended=True)), with PyMuPDF's
Rect, Point and Quad written as lists; its images (get_images(full=True)),
each image's Pixmap(doc, xref) as is and converted to RGB (size,
components, alpha, colourspace, digest, and the SHA-256 of tobytes("png") or
the error it raises), and where the page draws each (get_image_rects). Per
file, the xref table: xref_length() and every xref_object().
"""
import glob
import hashlib
import json
import os
import sys

import fitz


def render(page, dpi):
    pix = page.get_pixmap(dpi=dpi)
    return {
        "dpi": dpi, "width": pix.width, "height": pix.height,
        "png": hashlib.sha256(pix.tobytes("png")).hexdigest(),
        "samples": hashlib.sha256(pix.samples).hexdigest(),
    }


def plain(v):
    """v with PyMuPDF's Rect, Point, Quad and tuples as lists, bytes as
    their SHA-256."""
    if isinstance(v, (fitz.Rect, fitz.Point)):
        return [float(x) for x in v]
    if isinstance(v, fitz.Quad):
        return [plain(v.ul), plain(v.ur), plain(v.ll), plain(v.lr)]
    if isinstance(v, (bytes, bytearray)):
        return hashlib.sha256(v).hexdigest()
    if isinstance(v, dict):
        return {k: plain(x) for k, x in v.items()}
    if isinstance(v, (list, tuple)):
        return [plain(x) for x in v]
    return v


def pixmap(doc, xref, rgb):
    try:
        pix = fitz.Pixmap(doc, xref)
        if rgb:
            pix = fitz.Pixmap(fitz.csRGB, pix)
    except Exception as e:  # noqa: BLE001 - the Go side must fail too
        return {"xref": xref, "rgb": rgb, "error": f"{type(e).__name__}: {e}"}
    rec = {
        "xref": xref, "rgb": rgb, "width": pix.width, "height": pix.height, "n": pix.n,
        "alpha": bool(pix.alpha), "colorspace": pix.colorspace.name if pix.colorspace else "",
        "digest": pix.digest.hex(),
    }
    try:
        rec["png"] = hashlib.sha256(pix.tobytes("png")).hexdigest()
    except ValueError as e:
        rec["png_error"] = str(e)
    return rec


def page_record(page, dpis):
    try:
        sorted_text = page.get_text("text", sort=True)
    except ZeroDivisionError:
        sorted_text = None
    rec = {
        "text": page.get_text(),
        "sorted": sorted_text,
        "image": len(page.get_images(full=False)) > 0,
        "words": [w[4] for w in page.get_text("words")],
        "rect": list(page.rect),
        "image_boxes": [list(i["bbox"]) for i in page.get_image_info()],
        "blocks": plain(page.get_text("blocks")),
        "rawdict": plain(page.get_text("rawdict")),
        "drawings": plain(page.get_drawings()),
        "drawings_extended": plain(page.get_drawings(extended=True)),
        "images_full": plain(page.get_images(full=True)),
        "pixmaps": [pixmap(page.parent, i[0], rgb) for i in page.get_images(full=True) for rgb in (False, True)],
        "image_rects": [[i[0], plain(page.get_image_rects(i[0]))] for i in page.get_images(full=True)],
    }
    if dpis:
        rec["renders"] = [render(page, d) for d in dpis]
    return rec


def xref_object(doc, xref):
    try:
        return doc.xref_object(xref)
    except Exception:  # noqa: BLE001 - recorded as null, the Go side must fail too
        return None


def main(folder, dpis):
    out = os.path.join(folder, "pymupdf.jsonl")
    with open(out, "w", encoding="utf-8") as f:
        for path in sorted(glob.glob(os.path.join(folder, "*.pdf"))):
            rec = {"file": os.path.basename(path), "pymupdf": fitz.VersionBind}
            try:
                doc = fitz.open(path)
                rec["pages"] = [page_record(p, dpis) for p in doc]
                rec["xref_length"] = doc.xref_length()
                rec["xref_objects"] = [xref_object(doc, x) for x in range(1, doc.xref_length())]
            except Exception as e:  # noqa: BLE001 - recorded, the Go side must fail too
                rec["error"] = f"{type(e).__name__}: {e}"
            f.write(json.dumps(rec, ensure_ascii=False) + "\n")
    print(out)


if __name__ == "__main__":
    main(sys.argv[1], [int(d) for d in sys.argv[2].split(",")] if len(sys.argv) > 2 else [])
