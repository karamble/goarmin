# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
# goarmin figures: the rig. A minimal pose system for the Human Base Meshes primitive bodies (CC0, Blender
# Studio). Each body is a hierarchy of rigid parts with their pivots at the joints. A pose aims
# parts: the part's primary axis (towards a child joint) points along a direction, and its
# secondary axis (the front of the limb, or the top of the foot) along another. Parts that are
# not listed (shoulders, fingers, toes) follow their parents. Directions are in the world:
# the figure faces -Y, its left side is +X, up is +Z.
import math
import os

import bpy
from mathutils import Matrix, Vector

F, B, U, D = Vector((0, -1, 0)), Vector((0, 1, 0)), Vector((0, 0, 1)), Vector((0, 0, -1))
LT, RT = Vector((1, 0, 0)), Vector((-1, 0, 0))  # the figure's left and right


def ang(base, toward, deg):
    """base turned deg degrees towards toward."""
    r = math.radians(deg)
    return (base * math.cos(r) + toward * math.sin(r)).normalized()


# part: (the child joint that gives the primary axis, the secondary rest axis)
AIM = {"pelvis": ("chest", F), "belly": ("chest", F), "chest": ("neck", F), "neck": ("head", F),
       "head": (None, F)}
for s in ("L", "R"):
    AIM.update({f"arm_upper.{s}": (f"arm_lower.{s}", F), f"arm_lower.{s}": (f"hand.{s}", F),
                f"hand.{s}": (f"finger_middle.{s}", F), f"leg_upper.{s}": (f"leg_lower.{s}", F),
                f"leg_lower.{s}": (f"foot.{s}", F), f"foot.{s}": (f"toe_middle.{s}", U)})
for s in ("L", "R"):
    AIM[f"shoulder.{s}"] = (f"arm_upper.{s}", F)
ORDER = ["pelvis", "belly", "chest", "neck", "head"] + [f"{p}.{s}" for s in ("L", "R") for p in (
    "shoulder", "arm_upper", "arm_lower", "hand", "leg_upper", "leg_lower", "foot")]


def with_shoulders(spec):
    """Adds shoulder caps that turn partly with the upper arm, unless the spec has them."""
    out = dict(spec)
    for s, lateral in (("L", LT), ("R", RT)):
        arm = spec.get(f"arm_upper.{s}")
        if arm and f"shoulder.{s}" not in spec:
            out[f"shoulder.{s}"] = ((lateral + arm[0] * 0.8).normalized(), arm[1])
    return out


def parts(body):
    return {o["goarmin_part"]: o for o in bpy.data.objects if o.get("goarmin_body") == body}


def reset(body):
    """Puts the body back in its rest pose: each part's whole rest matrix (location, rotation and
    scale, mirroring included), as stored by setup."""
    ps = parts(body)
    for o in ps.values():
        r = o["rest_basis"]
        o.matrix_basis = Matrix([r[0:4], r[4:8], r[8:12], r[12:16]])
    bpy.context.view_layer.update()
    return ps


BUNDLE = os.environ.get("FIGURES_BUNDLE", "")  # the Human Base Meshes bundle .blend (build.py --bundle)
COLLECTIONS = {
    "male_stylized": "Body Male - Primitve (Stylized)",
    "male_realistic": "Body Male - Primitve (Realistic)",
    "female_stylized": "Body Female - Primitive (Stylized)",
    "female_realistic": "Body Female - Primitve (Realistic)",
}
FACE = ("ear", "eye", "eyelid", "nose")


def clean_name(n):
    import re
    n = n.replace("GEO-", "")
    for tag in ("_male_primitive_stylized", "_male_primitive_realistic", "_female_primitive_stylized",
                "_primitive_female_realistic", "_primitve_female_realistic", "_female_primitive_realistic",
                "_primitve"):
        n = n.replace(tag, "")
    return re.sub(r"\.\d{3}(?=\.[LR]$|$)", "", n).replace("teo_", "toe_")


def setup(scene_name="goarmin-figures", spacing=0.8, bodies=None, keep_face=False):
    """Appends the four primitive bodies fresh from the bundle into the scene, drops the face
    details, puts them in a row, tags each part and stores its rest matrix. Materials are left to
    the caller (goarmin_lime / _dim / _mid by side)."""
    import os
    if bpy.context.window is None:  # headless (blender -b): work in the current scene
        scn = bpy.context.scene
    else:
        scn = bpy.data.scenes.get(scene_name) or bpy.data.scenes.new(scene_name)
        bpy.context.window.scene = scn
    for cname in COLLECTIONS.values():  # what an earlier setup appended
        old = bpy.data.collections.get(cname)
        if old:
            for o in list(old.all_objects):
                bpy.data.objects.remove(o, do_unlink=True)
            bpy.data.collections.remove(old)
    chosen = {k: v for k, v in COLLECTIONS.items() if bodies is None or k in bodies}
    with bpy.data.libraries.load(os.path.expanduser(BUNDLE), link=False) as (src, dst):
        dst.collections = [c for c in src.collections if c in chosen.values()]
    for c in dst.collections:
        scn.collection.children.link(c)
    for i, (key, cname) in enumerate(chosen.items()):
        col = bpy.data.collections[cname]
        for o in list(col.all_objects):
            if not keep_face and any(clean_name(o.name).startswith(f) for f in FACE):
                bpy.data.objects.remove(o, do_unlink=True)
        root = [o for o in col.all_objects if o.parent is None][0]
        root.location.x, root.location.y = ((i - (len(chosen) - 1) / 2) * spacing), 0.0
        bpy.context.view_layer.update()
        for o in col.all_objects:
            o.rotation_mode = "QUATERNION"
            o["goarmin_body"], o["goarmin_part"] = key, clean_name(o.name)
            o["rest_basis"] = [v for row in o.matrix_basis for v in row]
            name = o["goarmin_part"]
            mat = "goarmin_lime_dim" if name.endswith(".L") else "goarmin_lime" if name.endswith(".R") else "goarmin_lime_mid"
            if bpy.data.materials.get(mat):
                o.data.materials.clear()
                o.data.materials.append(bpy.data.materials[mat])
    return scn


def frame(p, s):
    p = p.normalized()
    s = (s - p * s.dot(p)).normalized()
    return Matrix((p, s, p.cross(s))).transposed()


def rest_data(ps):
    """Primary and secondary world axes of each aimed part in the rest pose."""
    out = {}
    for name in ORDER:
        o = ps[name]
        child, sec = AIM[name]
        if child is None:  # the head: the neck's axis
            p0 = o.matrix_world.translation - ps["neck"].matrix_world.translation
        else:
            p0 = ps[child].matrix_world.translation - o.matrix_world.translation
        # The full rest matrix, scale and mirroring included: the right-side parts are mirrored
        # copies (negative scale), so the rotation must be applied to it as it is.
        out[name] = (p0.normalized(), sec, o.matrix_world.to_3x3())
    return out


# Contact groups for level(): part name prefixes; a group name may end in .L or .R.
GROUPS = {"hands": ("hand", "finger_", "thumb"), "feet": ("foot", "toe_"), "knees": ("leg_lower",),
          "forearms": ("arm_lower", "hand", "finger_", "thumb"), "head": ("head", "ear", "eye", "nose"),
          "back": ("pelvis", "belly", "chest", "shoulder", "neck"), "seat": ("pelvis",)}


def group_parts(ps, g):
    side = None
    if g.endswith((".L", ".R")):
        g, side = g[:-2], g[-1]
    pre = GROUPS.get(g, (g,))
    return [o for n, o in ps.items() if n.startswith(pre) and (side is None or n.endswith("." + side))]


def world_verts(objs):
    import numpy as np
    out = []
    for o in objs:
        n = len(o.data.vertices)
        co = np.empty(n * 3)
        o.data.vertices.foreach_get("co", co)
        m = np.array(o.matrix_world)
        out.append(co.reshape(n, 3) @ m[:3, :3].T + m[:3, 3])
    return np.concatenate(out)


def level(ps, a, b, axis="x", limit=40):
    """Turns the whole body about the world x (or y) axis, by the smallest angle that puts the
    lowest points of groups a and b at the same height (both then touch the floor)."""
    import numpy as np
    A, Bv = world_verts(group_parts(ps, a)), world_verts(group_parts(ps, b))

    def rot(deg):
        r = math.radians(deg)
        c, s = math.cos(r), math.sin(r)
        if axis == "x":
            return np.array([[1, 0, 0], [0, c, -s], [0, s, c]])
        return np.array([[c, 0, s], [0, 1, 0], [-s, 0, c]])

    def f(deg):
        R = rot(deg)
        return (Bv @ R.T)[:, 2].min() - (A @ R.T)[:, 2].min()

    grid = np.linspace(-limit, limit, 121)
    vals = [f(g) for g in grid]
    best = None
    for i in range(len(grid) - 1):
        if vals[i] * vals[i + 1] <= 0:
            lo, hi, flo = grid[i], grid[i + 1], vals[i]
            for _ in range(30):
                mid = (lo + hi) / 2
                fm = f(mid)
                if flo * fm <= 0:
                    hi = mid
                else:
                    lo, flo = mid, fm
            cand = (lo + hi) / 2
            if best is None or abs(cand) < abs(best):
                best = cand
    if best is None:
        best = float(grid[int(np.argmin(np.abs(vals)))])
    root = ps["pelvis"]
    root.matrix_world = Matrix(rot(best).tolist()).to_4x4() @ root.matrix_world
    bpy.context.view_layer.update()
    return best


def pose(body, spec, x=None):
    """Aims the body's parts as spec says, levels it on its contacts if the spec names them, then
    puts it on the floor (and at x)."""
    ps = reset(body)
    rest = rest_data(ps)
    spec = dict(spec)
    contacts = spec.pop("_contacts", None)
    lift = spec.pop("_lift", 0.0)  # metres above the floor (hanging, airborne)
    spec = with_shoulders(spec)
    for name in ORDER:
        if name not in spec:
            continue
        pt, st = spec[name]
        p0, s0, r0 = rest[name]
        q = frame(pt, st) @ frame(p0, s0).transposed()
        o = ps[name]
        o.matrix_world = Matrix.Translation(o.matrix_world.translation) @ (q @ r0).to_4x4()
        bpy.context.view_layer.update()
    if contacts:  # one (a, b[, axis]) or a list of them, applied in order
        for c in (contacts if isinstance(contacts[0], (list, tuple)) else [contacts]):
            level(ps, *c)
    root = ps["pelvis"]
    W = world_verts(list(ps.values()))
    root.location.z -= float(W[:, 2].min())
    root.location.y -= float(W[:, 1].min() + W[:, 1].max()) / 2
    root.location.z += lift
    if x is not None:
        root.location.x = x
    bpy.context.view_layer.update()
    return ps
