# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
# goarmin figures: review renders. Each pose renders twice (¾ from the front left, and from the
# figure's left side), framed automatically, in solid lime so the pose reads clearly. Contact
# sheets are put together with ImageMagick outside Blender.
import math
import os

import bpy
from mathutils import Vector

BODY = "male_realistic"
REVIEW = os.path.expanduser(os.environ.get("FIGURES_REVIEW", "figures-review"))
VIEWS = {"34": Vector((0.9, -1.0, 0.55)), "side": Vector((1.0, 0.0, 0.12))}


def review_materials():
    """Solid lime by side, for reading poses (the X-ray look is the viewer's)."""
    for name, hexcol in (("goarmin_review_R", "#c8f542"), ("goarmin_review_L", "#8da92e"), ("goarmin_review_C", "#aecd3a")):
        m = bpy.data.materials.get(name) or bpy.data.materials.new(name)
        m.use_nodes = True
        bsdf = next(n for n in m.node_tree.nodes if n.type == "BSDF_PRINCIPLED")
        h = hexcol.lstrip("#")
        c = [int(h[i:i + 2], 16) / 255 for i in (0, 2, 4)]
        bsdf.inputs["Base Color"].default_value = [x / 12.92 if x <= 0.04045 else ((x + 0.055) / 1.055) ** 2.4 for x in c] + [1]
        bsdf.inputs["Roughness"].default_value = 0.6
    for o in parts(BODY).values():
        p = o["goarmin_part"]
        side = "L" if p.endswith(".L") else "R" if p.endswith(".R") else "C"
        o.data.materials.clear()
        o.data.materials.append(bpy.data.materials[f"goarmin_review_{side}"])


def body_box():
    W = world_verts(list(parts(BODY).values()))
    return Vector(W.min(axis=0).tolist()), Vector(W.max(axis=0).tolist())


def shoot(path, view):
    scn = bpy.context.scene
    cam = bpy.data.objects["goarmin_cam"]
    lo, hi = body_box()
    c = (lo + hi) / 2
    r = (hi - lo).length / 2
    d = VIEWS[view].normalized()
    cam.data.lens = 50
    fov = 2 * math.atan(36 / 2 / 50) * 0.75  # the narrower (vertical, 4:3) field of view
    cam.location = c + d * (r / math.sin(fov / 2) * 1.02)
    cam.rotation_euler = (c - cam.location).to_track_quat("-Z", "Y").to_euler()
    scn.render.resolution_x, scn.render.resolution_y, scn.render.resolution_percentage = 480, 360, 100
    scn.render.filepath = path
    bpy.ops.render.render(write_still=True, scene=scn.name)


def review(name, spec, outdir=REVIEW):
    """Poses the body and renders both views: <outdir>/<name>_34.png and _side.png."""
    os.makedirs(outdir, exist_ok=True)
    pose(BODY, spec, x=0.0)
    for v in VIEWS:
        shoot(os.path.join(outdir, f"{name}_{v}.png"), v)


def shoot_quick(path, view="34", res=(320, 240)):
    """A quick framed render at a small size (the family review uses Workbench)."""
    scn = bpy.context.scene
    cam = bpy.data.objects["goarmin_cam"]
    lo, hi = body_box()
    lo.z = min(lo.z, 0.0)
    c = (lo + hi) / 2
    r = (hi - lo).length / 2
    d = VIEWS[view].normalized()
    cam.data.lens = 50
    fov = 2 * math.atan(36 / 2 / 50) * 0.75
    cam.location = c + d * (r / math.sin(fov / 2) * 1.02)
    cam.rotation_euler = (c - cam.location).to_track_quat("-Z", "Y").to_euler()
    scn.render.resolution_x, scn.render.resolution_y, scn.render.resolution_percentage = res[0], res[1], 100
    scn.render.filepath = path
    bpy.ops.render.render(write_still=True, scene=scn.name)
