# Third-party software

`pdftext` links MuPDF, and the MuPDF libraries it ships carry other people's code, fonts and character maps. Each is listed here with its licence, as its own licence file states it. The licence files themselves are in every release archive, under `licenses/`, copied by `scripts/build-mupdf.sh` from MuPDF 1.28.6's source.

Only what is in these builds is listed: MuPDF's source also holds Tesseract, Leptonica, Zint, ZXing-C++, curl and its DOCX writer (`extract`), which these builds do not include.

## MuPDF and PyMuPDF

| Software | Used for | Licence | Copyright |
|---|---|---|---|
| [MuPDF](https://mupdf.com) 1.28.6 | reading every PDF | AGPL-3.0, or a commercial licence from Artifex | Artifex Software, Inc., with patches from many outside developers (`licenses/MuPDF-CONTRIBUTORS`) |
| [PyMuPDF](https://github.com/pymupdf/PyMuPDF) 1.28.0 | `nx.c` and `sort.go` follow its code; its output is what the tests compare against | AGPL-3.0, or a commercial licence from Artifex | Artifex Software, Inc. and contributors |

## Libraries MuPDF bundles

| Library | Licence | Copyright |
|---|---|---|
| [brotli](https://github.com/google/brotli) | MIT | the Brotli Authors |
| [cmark-gfm](https://github.com/github/cmark-gfm) | BSD-2-Clause | John MacFarlane |
| [FreeType](https://freetype.org) 2.14.3 | FreeType License (FTL), chosen over GPLv2 | The FreeType Project |
| [gumbo-parser](https://github.com/google/gumbo-parser) | Apache-2.0 | Google |
| [HarfBuzz](https://harfbuzz.github.io) | "Old MIT" | Google, Ebrahim Byagowi, Facebook and others (`licenses/harfbuzz-COPYING`) |
| [jbig2dec](https://jbig2dec.com) | AGPL-3.0 | Artifex Software, Inc. |
| [Little CMS](https://www.littlecms.com) (lcms2, Artifex's multi-threaded fork) | MIT | Marti Maria Saguer |
| [libjpeg](https://ijg.org) | the IJG licence | Thomas G. Lane, Guido Vollbeding and the Independent JPEG Group |
| [MuJS](https://mujs.com) | ISC | Artifex Software, Inc. |
| [OpenJPEG](https://www.openjpeg.org) | BSD-2-Clause | Université catholique de Louvain and the OpenJPEG contributors |
| [zlib](https://zlib.net) | zlib licence | Jean-loup Gailly and Mark Adler |

Notices these licences ask for:

- Portions of this software are copyright © 2026 The FreeType Project (https://freetype.org). All rights reserved.
- This software is based in part on the work of the Independent JPEG Group.

## Fonts and character maps MuPDF bundles

The fonts are the ones MuPDF uses when a document does not embed its own; the CMap resources are the character maps that turn Japanese, Chinese and Korean character codes into text (the "CJK tables").

| Resource | Licence | Copyright |
|---|---|---|
| Noto fonts | SIL Open Font License 1.1 | Google and the Noto project authors |
| Source Han Serif | SIL Open Font License 1.1 | Adobe |
| Droid Sans Fallback | Apache-2.0 | The Android Open Source Project |
| Charis SIL | SIL Open Font License 1.1 | SIL International (Reserved Font Names "Charis" and "SIL") |
| URW base 35 (Nimbus, Standard Symbols, Dingbats) | SIL Open Font License 1.1 | (URW)++ Design & Development |
| Adobe CMap resources | Adobe's own licence ([cmap-resources](https://github.com/adobe-type-tools/cmap-resources)) | Adobe |

## Tools

| Tool | Used for | Licence |
|---|---|---|
| [ReportLab](https://www.reportlab.com) | making the test PDFs (`scripts/make_testdata.py`); not shipped | BSD |
| [PyMuPDF](https://github.com/pymupdf/PyMuPDF) | writing the reference answers (`scripts/pymupdf_golden.py`); not shipped | AGPL-3.0 |
