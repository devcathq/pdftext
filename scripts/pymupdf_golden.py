"""Write what PyMuPDF reads from every PDF in a folder, for TestAgainstPyMuPDF.

Usage: python scripts/pymupdf_golden.py <folder>   (writes <folder>/pymupdf.jsonl)

One JSON line per PDF: its page count, or the error PyMuPDF raised opening
it, and per page the plain text (page.get_text()), the sorted text
(page.get_text("text", sort=True), null where PyMuPDF raises), whether the
page lists an image, and its words' texts (page.get_text("words")).
"""
import glob
import json
import os
import sys

import fitz


def page_record(page):
    try:
        sorted_text = page.get_text("text", sort=True)
    except ZeroDivisionError:
        sorted_text = None
    return {
        "text": page.get_text(),
        "sorted": sorted_text,
        "image": len(page.get_images(full=False)) > 0,
        "words": [w[4] for w in page.get_text("words")],
    }


def main(folder):
    out = os.path.join(folder, "pymupdf.jsonl")
    with open(out, "w", encoding="utf-8") as f:
        for path in sorted(glob.glob(os.path.join(folder, "*.pdf"))):
            rec = {"file": os.path.basename(path), "pymupdf": fitz.VersionBind}
            try:
                doc = fitz.open(path)
                rec["pages"] = [page_record(p) for p in doc]
            except Exception as e:  # noqa: BLE001 - recorded, the Go side must fail too
                rec["error"] = f"{type(e).__name__}: {e}"
            f.write(json.dumps(rec, ensure_ascii=False) + "\n")
    print(out)


if __name__ == "__main__":
    main(sys.argv[1])
