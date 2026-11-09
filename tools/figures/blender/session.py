# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
# goarmin figures: work on the figures in a running Blender (docs/figures.md).
#
#   Blender › Scripting › Open tools/figures/blender/session.py › Run Script
#   (or start Blender with: blender -P tools/figures/blender/session.py)
#
# It builds a scene "goarmin-figures" with the mannequin, a camera, a light and a floor, and puts
# `fig` into Blender's Python console:
#
#   fig.show("core/51")        pose an exercise (key 0); fig.show("core/51", 1) for its next key
#   fig.find("cat cow")        exercises whose name, title or family matches
#   fig.play("core/51")        put its key poses on the timeline (Space plays the loop)
#   fig.review("core/51")      render it from ¾ and the side (paths printed)
#   fig.sheet(category="plank")   render many on contact sheets (also family=…, keys=[…])
#   fig.reload()               read the scripts again after editing them (own.py, families.py, …)
#   fig.export()               write the app's files, as make figures does
import builtins
import importlib.util
import os
import sys
import tempfile
import time

import bpy

MODULES = ("rig.py", "fk.py", "review.py", "own.py", "families.py", "catalog.py", "export.py", "slim_glb.py")


def _here():
    """This file's folder, also when Blender runs it from its text editor."""
    p = globals().get("__file__", "")
    if p and os.path.isfile(p):
        return os.path.dirname(os.path.abspath(p))
    for t in bpy.data.texts:
        fp = bpy.path.abspath(t.filepath) if t.filepath else ""
        if fp.endswith("session.py") and os.path.isfile(fp):
            return os.path.dirname(fp)
    if os.environ.get("FIGURES_DIR"):
        return os.environ["FIGURES_DIR"]
    raise RuntimeError("session.py: open it from the repository (tools/figures/blender), or set FIGURES_DIR")


HERE = _here()
REPO = os.path.abspath(os.path.join(HERE, "..", "..", ".."))
OUT = os.environ.get("FIGURES_REVIEW") or os.path.join(tempfile.gettempdir(), "goarmin-figures")


def _safe(label):
    """A family label as part of a file name (own:core/51 → own-core-51)."""
    return label.replace(":", "-").replace("/", "-")


class Session:
    def __init__(self):
        self.g = {}
        self._cat = None
        self.load()
        self.scene()

    # --- setup ---
    def load(self):
        """Runs the scripts in one namespace (self.g)."""
        os.environ["FIGURES_BUNDLE"] = os.path.join(HERE, "mannequin.blend")
        go_list = os.path.join(REPO, "bin", "figures-exercises.json")  # make figures writes it
        os.environ["FIGURES_EXPORT"] = OUT
        os.environ["FIGURES_EXERCISES"] = go_list if os.path.isfile(go_list) else os.path.join(REPO, "internal", "figures", "data", "catalog.json")
        g = {"__name__": "goarmin_figures"}
        for m in MODULES:
            path = os.path.join(HERE, m)
            exec(compile(open(path).read(), path, "exec"), g)
        self.g, self._cat = g, None

    def reload(self):
        """Reads the scripts again (after an edit) and keeps the scene."""
        self.load()
        self.g["review_materials"]()
        print("figures: scripts reloaded")

    def scene(self):
        """The mannequin with a camera, a light, a floor and the review look (Workbench)."""
        g = self.g
        scn = g["setup"](scene_name="goarmin-figures", bodies=["male_realistic"], keep_face=True)
        cam = bpy.data.objects.get("goarmin_cam")
        if cam is None:
            cam = bpy.data.objects.new("goarmin_cam", bpy.data.cameras.new("goarmin_cam"))
        if cam.name not in scn.collection.objects:
            scn.collection.objects.link(cam)
        scn.camera = cam
        sun = bpy.data.objects.get("goarmin_sun")
        if sun is None:
            light = bpy.data.lights.new("goarmin_sun", "SUN")
            light.energy = 3.0
            sun = bpy.data.objects.new("goarmin_sun", light)
            sun.rotation_euler = (0.8, 0.2, 0.6)
        if sun.name not in scn.collection.objects:
            scn.collection.objects.link(sun)
        floor = bpy.data.objects.get("goarmin_floor")
        if floor is None:
            me = bpy.data.meshes.new("goarmin_floor")
            me.from_pydata([(-3, -3, 0), (3, -3, 0), (3, 3, 0), (-3, 3, 0)], [], [(0, 1, 2, 3)])
            mat = bpy.data.materials.get("goarmin_floor") or bpy.data.materials.new("goarmin_floor")
            mat.diffuse_color = (0.05, 0.06, 0.07, 1)
            me.materials.append(mat)
            floor = bpy.data.objects.new("goarmin_floor", me)
        if floor.name not in scn.collection.objects:
            scn.collection.objects.link(floor)
        g["review_materials"]()
        try:
            scn.render.engine = "BLENDER_WORKBENCH"
        except TypeError:
            pass
        sh = scn.display.shading
        sh.light, sh.color_type, sh.show_cavity = "STUDIO", "MATERIAL", True
        g["reset"](g["BODY"])
        print(f"figures: scene {scn.name} ready; renders go to {OUT}")
        return scn

    # --- the catalogue ---
    def catalog(self):
        if self._cat is None:
            t = time.time()
            self._cat, errors = self.g["build"]()
            for e in errors:
                print("figures: pose error", e[0], e[1], e[3])
            print(f"figures: {len(self._cat)} exercises posed in {time.time() - t:.1f} s")
        return self._cat

    def get(self, key):
        """An exercise by key (core/51) or exact name (cat_cow)."""
        cat = self.catalog()
        if key in cat:
            return cat[key]
        for r in cat.values():
            if r["name"] == key:
                return r
        raise KeyError(f"no exercise {key!r} (fig.find(...) searches)")

    def find(self, text):
        """Exercises whose name, title or family contains every word of text."""
        words = text.lower().split()
        out = [(k, r["title"], r["label"]) for k, r in self.catalog().items()
               if all(w in f'{r["name"]} {r["title"].lower()} {r["label"]}' for w in words)]
        for row in out[:40]:
            print(*row)
        return [k for k, _, _ in out]

    # --- looking at poses ---
    def _clear_animation(self):
        for o in self.g["parts"](self.g["BODY"]).values():
            o.animation_data_clear()

    def show(self, key, k=0):
        """Poses the mannequin as an exercise's key k."""
        r = self.get(key)
        self._clear_animation()
        self.g["pose"](self.g["BODY"], r["keys"][k % len(r["keys"])], x=0.0)
        print(f'{r["title"]} ({r["category"]}/{r["number"]}): key {k % len(r["keys"]) + 1} of {len(r["keys"])}, {r["quality"]}, {r["label"]}')

    def play(self, key, step=24):
        """Keyframes the exercise's key poses (and back to the first) on the timeline."""
        r = self.get(key)
        g = self.g
        self._clear_animation()
        keys = r["keys"] + [r["keys"][0]] if len(r["keys"]) > 1 else r["keys"]
        last = {}
        for i, spec in enumerate(keys):
            ps = g["pose"](g["BODY"], spec, x=0.0)
            for name, o in ps.items():
                q = o.rotation_quaternion.copy()
                if name in last:
                    q.make_compatible(last[name])
                o.rotation_quaternion, last[name] = q, q
                o.keyframe_insert("rotation_quaternion", frame=1 + i * step)
                o.keyframe_insert("location", frame=1 + i * step)
        scn = bpy.context.scene
        scn.frame_start, scn.frame_end = 1, 1 + (len(keys) - 1) * step
        scn.frame_set(1)
        print(f'{r["title"]}: {len(r["keys"])} key poses on frames 1–{scn.frame_end}; Space plays them')

    def review(self, key, out=None):
        """Renders every key of an exercise from ¾ and the side."""
        r = self.get(key)
        out = out or os.path.join(OUT, "review")
        os.makedirs(out, exist_ok=True)
        self._clear_animation()
        paths = []
        for i, spec in enumerate(r["keys"]):
            self.g["pose"](self.g["BODY"], spec, x=0.0)
            for view in self.g["VIEWS"]:
                p = os.path.join(out, f'{_safe(r["label"])}__{r["category"]}-{r["number"]}_k{i}_{view}.png')
                self.g["shoot"](p, view)
                paths.append(p)
        print(*paths, sep="\n")
        return paths

    def sheet(self, category=None, family=None, keys=None, out=None):
        """Renders exercises (a category, a family or a list of keys) on contact sheets; each
        distinct pose set once."""
        cat = self.catalog()
        pick = [r for k, r in cat.items() if (keys and k in keys) or (category and r["category"] == category)
                or (family and r["label"] == family)]
        if not pick:
            raise ValueError("nothing to render: give category=…, family=… or keys=[…]")
        stamp = time.strftime("%Y%m%d-%H%M%S")
        out = out or os.path.join(OUT, f"sheet-{stamp}")
        os.makedirs(out, exist_ok=True)
        seen, files, t = set(), [], time.time()
        self._clear_animation()
        for r in pick:
            if r["sig"] in seen:
                continue
            seen.add(r["sig"])
            for i, spec in enumerate(r["keys"]):
                self.g["pose"](self.g["BODY"], spec, x=0.0)
                p = os.path.join(out, f'{_safe(r["label"])}__{r["name"]}_k{i}.png')
                self.g["shoot_quick"](p)
                files.append(p)
        spec = importlib.util.spec_from_file_location("goarmin_sheets", os.path.join(REPO, "tools", "figures", "sheets.py"))
        sheets = importlib.util.module_from_spec(spec)
        keep, sys.dont_write_bytecode = sys.dont_write_bytecode, True  # no __pycache__ in the repository
        try:
            spec.loader.exec_module(sheets)
        finally:
            sys.dont_write_bytecode = keep
        made = sheets.make_sheets(files, os.path.join(out, "sheet"))
        print(f"figures: {len(files)} renders in {time.time() - t:.0f} s")
        print(*made, sep="\n")
        return made

    # --- shipping ---
    def export(self):
        """Writes the app's files from the current scripts, as make figures does."""
        g = self.g
        self._clear_animation()
        cat = self.catalog()
        mesh = os.path.join(REPO, "web", "src", "figures", "assets", "mannequin.glb")
        tmp = mesh + ".tmp.glb"
        g["export_mesh"](tmp)
        g["main"](tmp, mesh)
        os.remove(tmp)
        n, sets, keys = g["export_catalog"](cat, os.path.join(REPO, "internal", "figures", "data"))
        print(f"figures: {n} exercises, {sets} pose sets, {keys} key poses written; check with git diff")


fig = Session()
builtins.fig = fig  # the Python console sees it as `fig`
bpy.app.driver_namespace["goarmin"] = fig
print("figures: `fig` is ready (fig.show, fig.find, fig.play, fig.review, fig.sheet, fig.reload, fig.export)")
