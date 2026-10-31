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
    }
    if dpis:
        rec["renders"] = [render(page, d) for d in dpis]
    return rec


def main(folder, dpis):
    out = os.path.join(folder, "pymupdf.jsonl")
    with open(out, "w", encoding="utf-8") as f:
        for path in sorted(glob.glob(os.path.join(folder, "*.pdf"))):
            rec = {"file": os.path.basename(path), "pymupdf": fitz.VersionBind}
            try:
                doc = fitz.open(path)
                rec["pages"] = [page_record(p, dpis) for p in doc]
            except Exception as e:  # noqa: BLE001 - recorded, the Go side must fail too
                rec["error"] = f"{type(e).__name__}: {e}"
            f.write(json.dumps(rec, ensure_ascii=False) + "\n")
    print(out)


if __name__ == "__main__":
    main(sys.argv[1], [int(d) for d in sys.argv[2].split(",")] if len(sys.argv) > 2 else [])
