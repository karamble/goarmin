# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
# goarmin figures: export for the app. Runs in Blender after the other scripts (same namespace).
#   export_mesh(path)      the mannequin in its rest pose, mesh only (glTF binary, Y up, no animation,
#                          no materials; every node carries goarmin_part in its extras)
#   export_catalog(dir)    catalog.json (every exercise) and poses.json (the distinct key-pose sets)
# A key pose is the root's position plus each moving part's local rotation, in glTF axes:
# node.position = t (root only), node.quaternion = q. Parts not listed keep their rest transform.
import json
import os

import bpy

EXPORT = os.path.expanduser(os.environ.get("FIGURES_EXPORT", "figures-export"))


def gl_quat(q):
    """Blender (w, x, y, z), Z up -> glTF [x, y, z, w], Y up."""
    return [round(q.x, 4), round(q.z, 4), round(-q.y, 4), round(q.w, 4)]


def gl_vec(v):
    return [round(v.x, 4), round(v.z, 4), round(-v.y, 4)]


def export_mesh(path=None):
    path = path or os.path.join(EXPORT, "mannequin.glb")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    ps = reset(BODY)
    ps["pelvis"].location.x = 0.0
    bpy.context.view_layer.update()
    mine = list(ps.values())
    for o in mine:
        o.animation_data_clear()
    bpy.ops.object.select_all(action="DESELECT")
    for o in mine:
        o.select_set(True)
    bpy.context.view_layer.objects.active = mine[0]
    bpy.ops.export_scene.gltf(filepath=path, export_format="GLB", use_selection=True, use_active_scene=True,
                              export_extras=True, export_materials="NONE", export_animations=False, export_yup=True)
    return path, os.path.getsize(path)


def local_trs(o):
    """The node's transform as the glTF exporter writes it: relative to the parent's world matrix
    (Blender's parent inverse baked in), decomposed into location, rotation and scale."""
    m = o.parent.matrix_world.inverted_safe() @ o.matrix_world if o.parent else o.matrix_world
    return m.decompose()


def key_record(spec):
    ps = pose(BODY, spec, x=0.0)
    rec = gl_vec(local_trs(ps["pelvis"])[0])
    for n in ORDER:
        rec += gl_quat(local_trs(ps[n])[1])
    return rec


def export_catalog(cat, outdir=EXPORT):
    os.makedirs(outdir, exist_ok=True)
    sets, exercises = {}, []
    for key, r in cat.items():
        if r["sig"] not in sets:
            sets[r["sig"]] = [key_record(k) for k in r["keys"]]
        exercises.append({"key": key, "category": r["category"], "category_num": r["category_num"], "number": r["number"],
                          "name": r["name"], "title": r["title"], "quality": r["quality"], "family": r["label"],
                          "pose": r["sig"], "mode": "hold" if len(r["keys"]) == 1 else "loop"})
    poses = {"version": 1, "parts": list(ORDER), "layout": "root position (3), then a quaternion [x, y, z, w] per part",
             "sets": sets}
    c = (",", ":")
    with open(os.path.join(outdir, "poses.json"), "w") as f:  # one pose set per line, for readable diffs
        head = {k: v for k, v in poses.items() if k != "sets"}
        f.write(json.dumps(head, separators=c)[:-1] + ',"sets":{\n')
        f.write(",\n".join(f"{json.dumps(s)}:{json.dumps(v, separators=c)}" for s, v in sets.items()))
        f.write("\n}}\n")
    with open(os.path.join(outdir, "catalog.json"), "w") as f:  # one exercise per line
        f.write('{"version":1,"exercises":[\n')
        f.write(",\n".join(json.dumps(e, separators=c, ensure_ascii=False) for e in exercises))
        f.write("\n]}\n")
    reset(BODY)
    return len(exercises), len(sets), sum(len(v) for v in sets.values())
