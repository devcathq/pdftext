"""Write the test PDFs in testdata/ (all made up here, no real documents).

Usage: python scripts/make_testdata.py testdata

- columns.pdf: a two-column page and a page of scattered labels, whose
  reading order depends on the sort (get_text("text", sort=True));
- rotated.pdf: the same text on a page rotated by 90 degrees;
- cjk.pdf: Japanese, Korean and Chinese pages in the PDF standard CJK fonts
  (no embedded font, no Unicode map), which only MuPDF's CJK tables read;
- many_pages.pdf: seven pages of a short invoice-like layout;
- images.pdf: a page covered by a scanned-looking image, a page with a logo,
  a note annotation and an image partly off the page, a rotated page with an
  image, and a blank page (for Render, ImageBoxes and Bounds);
- drawings.pdf: rules of several widths, caps and dashes, stroked, filled
  and filled-and-stroked boxes, circles and curves, closed quads, colours in
  grey, RGB and CMYK, a transparent fill, a clipped page with a blended fill
  and a rotated page
  (for Drawings);
- tables.pdf: ruled tables with a header row, an empty cell, a merged cell
  and a cell of two lines, beside running text (for Blocks, RawDict and the
  table finder);
- image_kinds.pdf: an RGB image with an alpha mask, a CMYK JPEG, a grey
  image, a logo placed twice and an image inside a Form XObject (for
  Images, Image and ImageRects).
"""
import os
import sys

from reportlab.lib.pagesizes import A4
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.cidfonts import UnicodeCIDFont
from reportlab.lib.utils import ImageReader
from reportlab.pdfgen import canvas
from PIL import Image, ImageDraw

for name in ("HeiseiKakuGo-W5", "HYSMyeongJo-Medium", "STSong-Light"):
    pdfmetrics.registerFont(UnicodeCIDFont(name))


def columns(c):
    left = ["Quarterly report", "Revenue rose 4% on", "higher rents across", "the three buildings."]
    right = ["Occupancy", "North tower 96%", "South tower 91%", "Annex 88%"]
    for i, (a, b) in enumerate(zip(left, right)):
        c.setFont("Helvetica", 11)
        c.drawString(60, 760 - 16 * i, a)
        c.drawString(320, 760 - 16 * i, b)
    c.showPage()
    for x, y, t in [(400, 700, "Total 1,250.00"), (60, 700, "Invoice No. 7"), (60, 640, "Due 30 Nov"),
                    (230, 671, "Paid by transfer"), (400, 600, "Page 2 of 2")]:
        c.setFont("Helvetica", 10)
        c.drawString(x, y, t)
    c.showPage()


def rotated(c):
    c.setPageRotation(90)
    c.setFont("Helvetica", 12)
    c.drawString(72, 500, "A rotated page reads as if it were upright.")
    c.drawString(72, 480, "Second line, same page.")
    c.showPage()


def cjk(c):
    pages = [
        ("HeiseiKakuGo-W5", ["請求書", "テスト内装株式会社 御中", "ご請求金額（税込）：¥165,000", "お支払期限：2026年11月30日"]),
        ("HYSMyeongJo-Medium", ["세금계산서", "공급자: 테스트상사", "합계금액: 1,100,000원", "작성일자: 2026-10-31"]),
        ("STSong-Light", ["发票", "购买方：测试公司", "金额合计：人民币 8,800 元", "开票日期：2026年10月31日"]),
    ]
    for font, lines in pages:
        for i, line in enumerate(lines):
            c.setFont(font, 14 if i == 0 else 11)
            c.drawString(60, 780 - 24 * i, line)
        c.showPage()


def many_pages(c):
    for n in range(1, 8):
        c.setFont("Helvetica-Bold", 13)
        c.drawString(60, 780, f"Statement page {n}")
        c.setFont("Helvetica", 10)
        for i in range(12):
            c.drawString(60, 750 - 18 * i, f"{n:02d}-{i:02d}  Service charge  unit {100 + i}")
            c.drawRightString(540, 750 - 18 * i, f"{(n * 37 + i * 11) % 900 + 100:,}.00")
        c.showPage()


def picture(w, h, mode, lines):
    """A made-up image: a frame, stripes and a few lines of text."""
    img = Image.new(mode, (w, h), "white")
    d = ImageDraw.Draw(img)
    d.rectangle([4, 4, w - 5, h - 5], outline="black", width=3)
    for y in range(40, h - 40, 60):
        d.line([20, y, w - 20, y], fill="gray", width=1)
    for i, t in enumerate(lines):
        d.text((30, 30 + 28 * i), t, fill="black")
    return ImageReader(img)


def images(c):
    w, h = A4
    c.drawImage(picture(620, 877, "L", ["SCANNED RECEIPT", "No. 0042", "Total 3,300"]), 0, 0, w, h)
    c.showPage()
    c.drawImage(picture(240, 120, "RGB", ["LOGO"]), 60, h - 140, 120, 60)
    c.drawImage(picture(200, 200, "RGB", ["half off the page"]), w - 100, 300, 200, 200)
    c.setFont("Helvetica", 11)
    c.drawString(60, h - 180, "A page with a logo and an image half off the page.")
    c.textAnnotation("A note", Rect=(400, h - 120, 420, h - 100), relative=0)
    c.showPage()
    c.setPageRotation(90)
    c.drawImage(picture(300, 200, "RGB", ["ROTATED", "PAGE"]), 100, 400, 300, 200)
    c.setFont("Helvetica", 11)
    c.drawString(100, 380, "Text under the image on a rotated page.")
    c.showPage()
    c.setPageRotation(0)
    c.showPage()


def drawings(c):
    w, h = A4
    for i, (width, cap, dash) in enumerate([(0.5, 0, None), (1, 1, None), (2, 2, None), (1.5, 0, [6, 3]), (1, 0, [1, 2, 3])]):
        c.setLineWidth(width)
        c.setLineCap(cap)
        c.setDash(dash or [])
        c.line(60, h - 60 - 20 * i, 300, h - 60 - 20 * i)
    c.setDash([])
    c.setLineWidth(1)
    c.setStrokeColorRGB(0.8, 0.1, 0.1)
    c.rect(60, 560, 120, 80, stroke=1, fill=0)
    c.setFillColorRGB(0.1, 0.4, 0.8)
    c.rect(200, 560, 120, 80, stroke=0, fill=1)
    c.setFillColorCMYK(0.1, 0.0, 0.6, 0.0)
    c.setStrokeGray(0.2)
    c.rect(340, 560, 120, 80, stroke=1, fill=1)
    c.setFillColorRGB(0.9, 0.6, 0.1)
    c.setFillAlpha(0.5)
    c.circle(120, 460, 50, stroke=1, fill=1)
    c.setFillAlpha(1)
    c.roundRect(200, 410, 120, 100, 15, stroke=1, fill=0)
    p = c.beginPath()
    p.moveTo(360, 420)
    p.lineTo(460, 430)
    p.lineTo(450, 500)
    p.lineTo(370, 490)
    p.close()
    c.drawPath(p, stroke=1, fill=0)
    p = c.beginPath()
    p.moveTo(480, 420)
    p.lineTo(540, 425)
    p.lineTo(535, 500)
    p.lineTo(485, 495)
    p.lineTo(480, 420)
    c.drawPath(p, stroke=1, fill=0)
    p = c.beginPath()
    p.moveTo(60, 340)
    p.curveTo(120, 400, 180, 280, 240, 340)
    c.drawPath(p, stroke=1, fill=0)
    c.setFont("Helvetica", 10)
    c.drawString(60, 300, "Rules, boxes and curves.")
    c.showPage()
    c.saveState()
    p = c.beginPath()
    p.rect(100, 400, 200, 200)
    c.clipPath(p, stroke=0, fill=0)
    c.setFillColorRGB(0.2, 0.7, 0.3)
    c.rect(50, 350, 300, 150, stroke=0, fill=1)
    c.setStrokeColorRGB(0, 0, 0)
    c.rect(150, 450, 300, 300, stroke=1, fill=0)
    c.restoreState()
    c.rect(60, 100, 200, 40, stroke=1, fill=0)
    c.saveState()
    c.setBlendMode("Multiply")
    c.setFillColorRGB(0.9, 0.2, 0.6)
    c.rect(300, 100, 120, 60, stroke=0, fill=1)
    c.restoreState()
    c.showPage()
    c.setPageRotation(90)
    c.setStrokeColorRGB(0, 0, 0)
    c.line(60, 700, 400, 700)
    c.rect(60, 500, 300, 150, stroke=1, fill=0)
    c.setFont("Helvetica", 11)
    c.drawString(70, 620, "A ruled box on a rotated page.")
    c.showPage()


def ruled_table(c, x, y, widths, height, rows, merge=None):
    """A table ruled on every cell edge; rows are lists of cell texts (a
    cell may hold two lines, split by \n). merge=(row, col) joins that cell
    with the one to its right."""
    total = sum(widths)
    c.setLineWidth(0.75)
    for r in range(len(rows) + 1):
        c.line(x, y - r * height, x + total, y - r * height)
    for r, row in enumerate(rows):
        cx = x
        for k, wd in enumerate(widths + [0]):
            if not (merge and merge == (r, k)):
                c.line(cx, y - r * height, cx, y - (r + 1) * height)
            cx += wd
        cx = x
        for k, cell in enumerate(row):
            for j, part in enumerate(cell.split("\n")):
                c.drawString(cx + 4, y - r * height - 13 - 11 * j, part)
            cx += widths[k]


def tables(c):
    w, h = A4
    c.setFont("Helvetica-Bold", 13)
    c.drawString(60, h - 60, "Schedule 2: Fees")
    c.setFont("Helvetica", 10)
    c.drawString(60, h - 80, "5.1. The Manager is paid the fees below, in arrears, each quarter.")
    ruled_table(c, 60, h - 100, [150, 120, 120], 30, [
        ["Fee", "Rate", "Basis"],
        ["Management fee", "1.50%", "NAV"],
        ["Performance fee", "15.00%", ""],
        ["Administration\nfee", "0.10%", "NAV"],
    ])
    c.drawString(60, h - 260, "5.2. Fees are charged to the Fund and paid in cash.")
    ruled_table(c, 60, h - 290, [100, 100, 100, 100], 24, [
        ["Year", "Q1", "Q2", "Q3"],
        ["2025", "120", "130", "125"],
        ["2026", "140", "merged across", ""],
    ], merge=(2, 3))
    c.showPage()
    c.setFont("Helvetica", 10)
    c.drawString(60, h - 60, "Unit schedule")
    c.setFillColorRGB(0.85, 0.85, 0.85)
    c.rect(60, h - 100, 400, 24, stroke=0, fill=1)
    c.setFillColorRGB(0, 0, 0)
    ruled_table(c, 60, h - 76, [100, 100, 100, 100], 24, [
        ["Unit", "Area", "Tenant", "Rent"],
        ["101", "52.3", "A Co.", "1,200"],
        ["102", "48.1", "B Co.", "1,100"],
        ["201", "60.0", "", "0"],
    ])
    c.showPage()


def image_kinds(c):
    import io
    w, h = A4
    rgba = Image.new("RGBA", (120, 80), (0, 0, 0, 0))
    d = ImageDraw.Draw(rgba)
    d.ellipse([10, 10, 110, 70], fill=(200, 30, 30, 255))
    d.rectangle([40, 30, 80, 50], fill=(30, 30, 200, 128))
    c.drawImage(ImageReader(rgba), 60, h - 160, 120, 80, mask="auto")
    cmyk = Image.new("CMYK", (90, 60), (0, 128, 255, 0))
    ImageDraw.Draw(cmyk).rectangle([10, 10, 80, 50], fill=(255, 0, 0, 64))
    buf = io.BytesIO()
    cmyk.save(buf, "JPEG", quality=90)
    buf.seek(0)
    c.drawImage(ImageReader(buf), 220, h - 160, 90, 60)
    c.drawImage(picture(100, 100, "L", ["GREY"]), 340, h - 160, 100, 100)
    logo = picture(160, 80, "RGB", ["LOGO"])
    c.drawImage(logo, 60, h - 320, 80, 40)
    c.drawImage(logo, 300, h - 320, 160, 80)
    c.beginForm("stamp")
    c.drawImage(picture(80, 80, "RGB", ["FORM"]), 0, 0, 80, 80)
    c.endForm()
    c.saveState()
    c.translate(60, 300)
    c.doForm("stamp")
    c.restoreState()
    c.setFont("Helvetica", 10)
    c.drawString(60, 280, "Images of several kinds.")
    c.showPage()


def main(out):
    os.makedirs(out, exist_ok=True)
    for name, fill in [("columns", columns), ("rotated", rotated), ("cjk", cjk), ("many_pages", many_pages),
                       ("images", images), ("drawings", drawings), ("tables", tables),
                       ("image_kinds", image_kinds)]:
        c = canvas.Canvas(os.path.join(out, name + ".pdf"), pagesize=A4, invariant=1)
        fill(c)
        c.save()


if __name__ == "__main__":
    main(sys.argv[1])
