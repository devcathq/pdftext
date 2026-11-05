"""Run testdata/write_cases.json with PyMuPDF, for TestWritingAgainstPyMuPDF.

Usage: python scripts/pymupdf_write_golden.py testdata
(writes testdata/pymupdf_write.jsonl)

Each case is a list of operations on a document: fitz.open() or a test PDF,
new_page, draw_rect, draw_line, insert_text, insert_textbox, a Story laid
out through a DocumentWriter (as the HTML-to-PDF engine does, then
reopened), metadata and set_metadata, a page subset as a new PDF, and
Font.text_length. One JSON line per case: each operation's return value, and
the resulting PDF's pages: their text (get_text()) and the SHA-256 of their
pixels drawn at 72 DPI. The PDF bytes themselves are not compared; pdftext's
need not equal PyMuPDF's, only what they show.
"""
import hashlib
import io
import json
import os
import sys

import fitz

LONG = "".join(
    f"<p>Paragraph {i}: the second group runs over several pages so the story places it more than once.</p>"
    for i in range(1, 61)
)


def page_records(doc):
    out = []
    for page in doc:
        pix = page.get_pixmap(dpi=72)
        out.append({"text": page.get_text(), "samples": hashlib.sha256(pix.samples).hexdigest()})
    return out


def opts(op, keys):
    return {k: op[k] for k in keys if k in op}


def run(case, folder):
    doc = None
    returns = []
    for op in case["ops"]:
        kind, ret = op["op"], None
        if kind == "new":
            doc = fitz.open()
        elif kind == "open":
            doc = fitz.open(os.path.join(folder, op["file"]))
        elif kind == "reopen":
            doc = fitz.open(stream=doc.tobytes(), filetype="pdf")
        elif kind == "new_page":
            doc.new_page(width=op["w"], height=op["h"])
        elif kind == "draw_rect":
            kw = opts(op, ["color", "fill", "width"])
            doc[op["page"]].draw_rect(fitz.Rect(op["rect"]), **kw)
        elif kind == "draw_line":
            kw = opts(op, ["color", "width"])
            doc[op["page"]].draw_line(fitz.Point(op["p1"]), fitz.Point(op["p2"]), **kw)
        elif kind == "insert_text":
            kw = opts(op, ["fontname", "fontsize", "color", "render_mode", "fill_opacity"])
            ret = doc[op["page"]].insert_text(fitz.Point(op["point"]), op["text"], **kw)
        elif kind == "insert_textbox":
            kw = opts(op, ["fontname", "fontsize", "color", "align"])
            ret = doc[op["page"]].insert_textbox(fitz.Rect(op["rect"]), op["text"], **kw)
        elif kind == "story":
            buf = io.BytesIO()
            writer = fitz.DocumentWriter(buf)
            for html in op["groups"]:
                story = fitz.Story(html=html.replace("LONGTEXT", LONG), user_css=op["css"])
                more = 1
                while more:
                    dev = writer.begin_page(fitz.Rect(op["mediabox"]))
                    more, _ = story.place(fitz.Rect(op["rect"]))
                    story.draw(dev)
                    writer.end_page()
            writer.close()
            doc = fitz.open(stream=buf.getvalue(), filetype="pdf")
        elif kind == "metadata":
            ret = dict(doc.metadata)
        elif kind == "set_metadata":
            doc.set_metadata(op["meta"])
        elif kind == "extract_pages":
            out = fitz.open()
            for p in op["pages"]:
                out.insert_pdf(doc, from_page=p, to_page=p)
            doc = fitz.open(stream=out.tobytes(), filetype="pdf")
        elif kind == "text_length":
            ret = fitz.Font(op["font"]).text_length(op["text"], fontsize=op["size"])
        returns.append(ret)
    rec = {"name": case["name"], "returns": returns}
    if doc is not None:
        doc = fitz.open(stream=doc.tobytes(), filetype="pdf")
        rec["pages"] = page_records(doc)
    return rec


def main(folder):
    cases = json.load(open(os.path.join(folder, "write_cases.json"), encoding="utf-8"))
    out = os.path.join(folder, "pymupdf_write.jsonl")
    with open(out, "w", encoding="utf-8") as f:
        for case in cases:
            f.write(json.dumps(run(case, folder), ensure_ascii=False) + "\n")
    print(out)


if __name__ == "__main__":
    main(sys.argv[1])
