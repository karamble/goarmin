# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
# goarmin figures: the whole catalogue. Runs in Blender after rig, fk, review, own and
# families (same namespace). build() gives every exercise of Garmin's list its key poses:
# own (the user's 45, posed one by one), family (a movement family matched by name) or category
# (the category's fallback family).
import hashlib
import json
import os
import traceback

EXERCISES = os.environ.get("FIGURES_EXERCISES", "")  # go run ./tools/figures/exercises > exercises.json


def title_of(name):
    small = {"and", "with", "to", "on", "of", "the", "a", "in", "or"}
    words = name.replace("_", " ").split()
    return " ".join(w if (i and w in small) else w[:1].upper() + w[1:] for i, w in enumerate(words))


def spec_sig(k):
    items = []
    for n in sorted(k):
        v = k[n]
        if n in ("_contacts", "_lift"):
            items.append((n, json.dumps(v)))
            continue
        d, s = v
        items.append((n, tuple(round(x, 2) for x in (*d, *s))))
    return items


def keys_sig(keys):
    return hashlib.sha1(repr([spec_sig(k) for k in keys]).encode()).hexdigest()[:12]


def build():
    ex = json.load(open(EXERCISES))
    if isinstance(ex, dict):  # internal/figures/data/catalog.json carries the same fields
        ex = ex["exercises"]
    out, errors = {}, []
    for e in ex:
        cat, num, name = e["category"], e["number"], e["name"]
        key = f"{cat}/{num}"
        rec = {"category": cat, "category_num": e["category_num"], "number": num, "name": name, "title": title_of(name)}
        if key in USER:
            title, keys = USER[key]
            rec.update(title=title, quality="own", label="own:" + key, keys=list(keys))
        else:
            fn, label, quality, c = classify(cat, name)
            try:
                keys = fn(c)
                if not keys:
                    raise ValueError("no keys")
            except Exception as err:
                errors.append((key, name, label, repr(err), traceback.format_exc(limit=3)))
                label, fn = CATEGORY_DEFAULT.get(cat, ("mountain", Yg("mountain")))
                quality, keys = "category", fn(c)
            rec.update(quality=quality, label=label, keys=list(keys))
        rec["sig"] = keys_sig(rec["keys"])
        out[key] = rec
    return out, errors
