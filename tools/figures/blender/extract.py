# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
# goarmin figures: writes the mannequin asset (make figures-asset). From Blender Studio's Human Base
# Meshes bundle (v1.4.1, CC0) it keeps only the body the figures use, so the repository holds a
# few MB instead of the bundle's 49:
#   blender -b --factory-startup --python-exit-code 1 -P tools/figures/blender/extract.py -- \
#     --bundle human_base_meshes_bundle.blend --out tools/figures/blender/mannequin.blend
import argparse
import os
import sys

import bpy

COLLECTION = "Body Male - Primitve (Realistic)"  # the bundle's own spelling


def main():
    ap = argparse.ArgumentParser(prog="extract.py")
    ap.add_argument("--bundle", required=True, help="the Human Base Meshes bundle .blend")
    ap.add_argument("--out", required=True, help="the trimmed .blend to write")
    args = ap.parse_args(sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else [])
    with bpy.data.libraries.load(os.path.abspath(args.bundle), link=False) as (src, dst):
        if COLLECTION not in src.collections:
            sys.exit(f"extract: no collection {COLLECTION!r} in {args.bundle}")
        dst.collections = [COLLECTION]
    coll = dst.collections[0]
    bpy.data.libraries.write(os.path.abspath(args.out), {coll}, path_remap="NONE", compress=True)
    print(f"extract: {COLLECTION} ({len(coll.all_objects)} objects) -> {args.out}, {os.path.getsize(args.out)} bytes")


main()
