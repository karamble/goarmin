# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
# goarmin figures: a pose language on anatomical joint angles, over rig.py's aim system.
#
# A pose is a body orientation plus joint angles in degrees:
#   orient     standing | prone | supine | side_left | side_right | inverted, or (up, front) vectors
#   pelvis     (flex, bend, twist): the pelvis against the orientation (a forward fold tilts it)
#   spine      (flex, bend, twist): trunk forward bend, side bend to the left, twist to the left,
#              shared by belly (40 %) and chest (60 %)
#   neck, head (flex, bend, twist)
#   arms       L / R: flex (raise forwards), abd (raise sideways), elbow (bend), pron (thumb inwards),
#              wrist (bend, palm side)
#   legs       L / R: flex (hip, thigh forwards), abd (sideways), rot (turn outwards), knee (bend),
#              ankle (toes down: plantar flexion; negative pulls the toes up)
# Frames are 3×3 matrices whose columns are the segment's front, up and left in the world. A limb
# points along its frame's -up (hanging), the foot along its front.
import math

from mathutils import Matrix, Vector

# Body orientations: (up, front) in the world; the head points along up.
ORIENT = {
    "standing": (U, F),
    "prone": (F, D),       # face down, head towards -Y
    "supine": (F, U),      # on the back, head towards -Y
    "side_left": (F, RT),  # on the left side (left down), head towards -Y, belly towards -X
    "side_right": (F, LT),
    "inverted": (D, F),
}


def base_frame(up, front):
    up = up.normalized()
    front = (front - up * front.dot(up)).normalized()
    left = up.cross(front)
    return Matrix((front, up, left)).transposed()


def rot(m, axis, deg):
    """m rotated about its own axis (0 front, 1 up, 2 left) by deg. The frame (front, up, left)
    is left-handed (left = up × front), so the angle is negated against Blender's right-handed
    rotation: the signs below then read as described in the header (checked on renders)."""
    a = Vector((m[0][axis], m[1][axis], m[2][axis]))
    return Matrix.Rotation(math.radians(-deg), 3, a) @ m


def fbt(m, flex=0, bend=0, twist=0):
    """Forward flexion, side bend to the left, twist to the left."""
    m = rot(m, 1, -twist)   # front turns towards the left
    m = rot(m, 0, bend)     # up tilts towards the left
    return rot(m, 2, -flex)  # up tilts towards the front


def col(m, i):
    return Vector((m[0][i], m[1][i], m[2][i]))


def _arm(chest, side, a):
    s = 1 if side == "L" else -1
    m = rot(chest, 2, a.get("flex", 0))        # raise forwards: -up towards front
    m = rot(m, 0, -s * a.get("abd", 0))        # raise sideways, away from the body
    m = rot(m, 1, -s * a.get("rot", 0))        # upper arm turns outwards
    upper = m
    fore = rot(rot(upper, 2, a.get("elbow", 0)), 1, s * a.get("pron", 0))
    hand = rot(fore, 2, a.get("wrist", 0))
    return {f"arm_upper.{side}": (-col(upper, 1), col(upper, 0)),
            f"arm_lower.{side}": (-col(fore, 1), col(fore, 0)),
            f"hand.{side}": (-col(hand, 1), col(hand, 0))}


def _leg(pelvis, side, g):
    s = 1 if side == "L" else -1
    m = rot(pelvis, 2, g.get("flex", 0))       # thigh forwards
    m = rot(m, 0, -s * g.get("abd", 0))        # thigh sideways, away from the body
    m = rot(m, 1, -s * g.get("rot", 0))        # turned outwards
    thigh = m
    shin = rot(thigh, 2, -g.get("knee", 0))    # shin bends backwards
    foot = rot(shin, 2, -g.get("ankle", 0))    # toes down
    return {f"leg_upper.{side}": (-col(thigh, 1), col(thigh, 0)),
            f"leg_lower.{side}": (-col(shin, 1), col(shin, 0)),
            f"foot.{side}": (col(foot, 0), col(foot, 1))}


def P(orient="standing", pelvis=(0, 0, 0), spine=(0, 0, 0), neck=(0, 0, 0), head=(0, 0, 0),
      L=None, R=None, LL=None, RL=None, arms=None, legs=None, hands=None, feet=None, aim=None,
      contacts=None):
    """A pose as an aim spec. arms/legs apply to both sides unless L/R (arms) or LL/RL (legs) say
    otherwise; their values are dicts as described above. hands/feet override the hand and foot
    in world terms, for support on the floor: {"L": (direction, thumb or top), "R": (...)} or one
    pair for both, where "medial"/"lateral" as the second item mean towards/away from the body's
    midline.
    aim sets parts straight in world terms, after everything else: {part: (direction, second)},
    where a limb part without .L/.R means both sides (supporting limbs: an arm straight down to
    the floor, a shin flat on it). contacts=(group_a, group_b[, axis]) tilts the whole body about
    the world x (or y) axis until both groups touch the floor (see rig.level)."""
    up, front = ORIENT[orient] if isinstance(orient, str) else orient
    base = base_frame(up, front)
    pel = fbt(base, *pelvis)
    belly = fbt(pel, *(v * 0.4 for v in spine))
    chest = fbt(belly, *(v * 0.6 for v in spine))
    nk = fbt(chest, *neck)
    hd = fbt(nk, *head)
    spec = {"pelvis": (col(pel, 1), col(pel, 0)), "belly": (col(belly, 1), col(belly, 0)),
            "chest": (col(chest, 1), col(chest, 0)), "neck": (col(nk, 1), col(nk, 0)),
            "head": (col(hd, 1), col(hd, 0))}
    a, g = arms or {}, legs or {}
    spec.update(_arm(chest, "L", {**a, **(L or {})}))
    spec.update(_arm(chest, "R", {**a, **(R or {})}))
    spec.update(_leg(pel, "L", {**g, **(LL or {})}))
    spec.update(_leg(pel, "R", {**g, **(RL or {})}))
    left = col(base, 2)
    for kind, over in (("hand", hands), ("foot", feet)):
        if not over:
            continue
        for side in ("L", "R"):
            d, second = over.get(side, over.get("both")) if isinstance(over, dict) else over
            medial = -left if side == "L" else left
            if isinstance(second, str):
                second = medial if second == "medial" else -medial
            spec[f"{kind}.{side}"] = (d, second)
    for key, (d, second) in (aim or {}).items():
        names = [key] if key.endswith((".L", ".R")) or key in TRUNK else [f"{key}.L", f"{key}.R"]
        for n in names:
            sec = second
            if isinstance(sec, str):
                medial = -left if n.endswith(".L") else left
                sec = medial if sec == "medial" else -medial
            spec[n] = (d, sec)
    if contacts:
        spec["_contacts"] = contacts
    return spec


TRUNK = ("pelvis", "belly", "chest", "neck", "head")
