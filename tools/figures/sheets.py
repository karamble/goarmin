#!/usr/bin/env python3
# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
"""Contact sheets of figure renders, for reviewing many poses at a glance (ImageMagick montage).

    python3 tools/figures/sheets.py DIR [OUT_PREFIX]

Puts every PNG in DIR on sheets of 60 (10 across), each labelled with its file name (the review
renders are named <label>__<exercise>_k<i>.png), and prints the sheets' paths. session.py's
sheet() calls make_sheets() directly.
"""
import glob
import os
import subprocess
import sys

PER, COLS = 60, 10


def label_of(path):
    name = os.path.splitext(os.path.basename(path))[0]
    return name.replace("__", " · ")[:34]


def make_sheets(files, out_prefix, per=PER, cols=COLS):
    """Writes out_prefix_01.png, _02.png, … and returns their paths."""
    out = []
    for n in range(0, len(files), per):
        chunk = files[n:n + per]
        args = []
        for f in chunk:
            args += ["-label", label_of(f), f]
        path = f"{out_prefix}_{n // per + 1:02d}.png"
        subprocess.run(["montage", *args, "-tile", f"{cols}x", "-geometry", "170x128+1+1", "-background", "#161b22",
                        "-fill", "#c9d1d9", "-pointsize", "9", path], check=True)
        out.append(path)
    return out


def main(argv):
    if not argv:
        sys.exit(__doc__)
    d = argv[0]
    files = sorted(glob.glob(os.path.join(d, "*.png")))
    if not files:
        sys.exit(f"sheets: no PNG in {d}")
    for p in make_sheets(files, argv[1] if len(argv) > 1 else os.path.join(d, "sheet")):
        print(p)


if __name__ == "__main__":
    main(sys.argv[1:])
