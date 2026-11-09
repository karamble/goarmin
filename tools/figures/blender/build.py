# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
# goarmin figures: the headless build (make figures). Poses every exercise of Garmin's list on the
# mannequin and writes what the app ships:
#   blender -b --factory-startup --python-exit-code 1 -P tools/figures/blender/build.py -- \
#     --bundle human_base_meshes_bundle.blend --exercises exercises.json \
#     --data internal/figures/data --mesh web/src/figures/assets/mannequin.glb
import argparse
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
MODULES = ("rig.py", "fk.py", "review.py", "own.py", "families.py", "catalog.py", "export.py", "slim_glb.py")


def main():
    ap = argparse.ArgumentParser(prog="build.py")
    ap.add_argument("--bundle", required=True, help="Human Base Meshes v1.4.1 bundle .blend (CC0, Blender Studio)")
    ap.add_argument("--exercises", required=True, help="go run ./tools/figures/exercises output")
    ap.add_argument("--data", required=True, help="directory for catalog.json and poses.json")
    ap.add_argument("--mesh", required=True, help="path of the mannequin .glb")
    args = ap.parse_args(sys.argv[sys.argv.index("--") + 1:] if "--" in sys.argv else [])
    os.environ["FIGURES_BUNDLE"] = os.path.abspath(args.bundle)
    os.environ["FIGURES_EXERCISES"] = os.path.abspath(args.exercises)
    g = {"__name__": "goarmin_figures"}
    for m in MODULES:
        path = os.path.join(HERE, m)
        exec(compile(open(path).read(), path, "exec"), g)
    g["setup"](bodies=["male_realistic"], keep_face=True)
    catalog, errors = g["build"]()
    for e in errors:
        print("figures: pose error", e[0], e[1], e[3], file=sys.stderr)
    if errors:
        sys.exit(1)
    tmp = args.mesh + ".tmp.glb"
    g["export_mesh"](tmp)
    g["main"](tmp, args.mesh)  # slim_glb: keep only goarmin_part in the extras
    os.remove(tmp)
    n, sets, keys = g["export_catalog"](catalog, args.data)
    own = sum(1 for r in catalog.values() if r["quality"] == "own")
    fam = sum(1 for r in catalog.values() if r["quality"] == "family")
    print(f"figures: {n} exercises ({own} own, {fam} family, {n - own - fam} category), {sets} pose sets, {keys} key poses")


main()
