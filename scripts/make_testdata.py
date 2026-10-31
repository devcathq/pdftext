"""Write the test PDFs in testdata/ (all made up here, no real documents).

Usage: python scripts/make_testdata.py testdata

- columns.pdf: a two-column page and a page of scattered labels, whose
  reading order depends on the sort (get_text("text", sort=True));
- rotated.pdf: the same text on a page rotated by 90 degrees;
- cjk.pdf: Japanese, Korean and Chinese pages in the PDF standard CJK fonts
  (no embedded font, no Unicode map), which only MuPDF's CJK tables read;
- many_pages.pdf: seven pages of a short invoice-like layout.
"""
import os
import sys

from reportlab.lib.pagesizes import A4
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.cidfonts import UnicodeCIDFont
from reportlab.pdfgen import canvas

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


def main(out):
    os.makedirs(out, exist_ok=True)
    for name, fill in [("columns", columns), ("rotated", rotated), ("cjk", cjk), ("many_pages", many_pages)]:
        c = canvas.Canvas(os.path.join(out, name + ".pdf"), pagesize=A4, invariant=1)
        fill(c)
        c.save()


if __name__ == "__main__":
    main(sys.argv[1])
