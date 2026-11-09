# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
# goarmin figures: movement families for Garmin's whole exercise list.
# Runs inside Blender after rig.py, fk.py and own.py (same namespace).
# A family is a function(c) -> list of key specs; c holds flags read from the exercise name.
# The mannequin holds no equipment and has no bench, box or bar: it shows the body's movement.
import re

from mathutils import Vector


def V(*xs):
    """A direction from weighted world vectors, normalized. Two spellings:
    weights before vectors, V(F, .5, U) = F + 0.5 U (ends with a vector), or vector-weight pairs,
    V(F, 1, U, .5) = F + 0.5 U (ends with a number)."""
    out = Vector((0, 0, 0))
    if xs and isinstance(xs[-1], (int, float)):
        for i in range(0, len(xs), 2):
            out += xs[i] * xs[i + 1]
        return out.normalized()
    w = 1.0
    for x in xs:
        if isinstance(x, (int, float)):
            w = x
        else:
            out += x * w
            w = 1.0
    return out.normalized()


def lw(p, thigh, shin, ankle=None):
    """fk leg angles from world angles: thigh and shin turned forward of straight down (degrees),
    with the pelvis flexed p; the foot flat on the floor unless ankle is given."""
    return {"flex": thigh + p, "knee": thigh - shin, "ankle": shin if ankle is None else ankle}


def sym(fn):
    """Aims for both sides from fn(lat) -> {part: (dir, sec)}, lat the side's outward vector."""
    out = {}
    for s, lat in (("L", LT), ("R", RT)):
        for part, v in fn(lat).items():
            out[f"{part}.{s}"] = v
    return out


def one(side, d):
    return {f"{k}.{side}": v for k, v in d.items()}


def limb(upper, lower=None, hand=None, sec=F, hsec=None):
    """An arm as aims: upper arm, forearm (default straight) and hand (default in line)."""
    lower = lower or upper
    return {"arm_upper": (upper, sec), "arm_lower": (lower, sec), "hand": (hand or lower, hsec or sec)}


def leg(thigh, shin=None, foot=None, sec=F, fsec=U):
    shin = shin or thigh
    return {"leg_upper": (thigh, sec), "leg_lower": (shin, sec), "foot": (foot or F, fsec)}


def mirror_vec(v):
    return Vector((-v.x, v.y, v.z))


def _swap(n):
    return n[:-1] + ("R" if n.endswith(".L") else "L") if n.endswith((".L", ".R")) else n


def mirror(spec):
    """The same key pose with left and right swapped."""
    out = {}
    for n, v in spec.items():
        if n == "_lift":
            out[n] = v
            continue
        if n == "_contacts":
            if isinstance(v[0], (list, tuple)):
                out[n] = [tuple(_swap(x) if i < 2 else x for i, x in enumerate(c)) for c in v]
            else:
                out[n] = tuple(_swap(x) if i < 2 else x for i, x in enumerate(v))
            continue
        d, s = v
        out[_swap(n)] = (mirror_vec(d), mirror_vec(s))
    return out


def merge(*kws):
    """Merges P keyword dicts; aim and leg dicts are merged, the rest overridden."""
    out = {}
    for kw in kws:
        for k, v in kw.items():
            if k in ("aim", "LL", "RL", "legs", "arms", "L", "R") and k in out:
                out[k] = {**out[k], **v}
            else:
                out[k] = v
    return out


# --- Arm positions (world aims, for an upright trunk; F is where the figure faces) ---
DOWN = sym(lambda lat: limb(V(D, .08, lat), sec=F))
HANDS_HIPS = sym(lambda lat: limb(V(D, .5, lat, .2, B), V(D, .2, F, -.6 if lat.x > 0 else .6, LT), sec=F))
FORWARD = sym(lambda lat: limb(F, sec=U, hsec=V(-lat)))
OVERHEAD = sym(lambda lat: limb(V(U, .15, lat), sec=V(-lat)))
T_ARMS = sym(lambda lat: limb(lat, sec=F))
AT_SHOULDERS = sym(lambda lat: limb(V(lat, .35, D, .15, F), U, sec=F))  # press start
GUARD = sym(lambda lat: limb(V(D, .5, F, .25, lat), V(U, .6, F, .3, -lat), sec=V(F, .3, -lat)))
CHEST = sym(lambda lat: limb(V(D, .7, F, .25, lat), V(-lat, .5, U, .4, F), sec=F))  # goblet, hands at the chest
FRONT_RACK = sym(lambda lat: limb(V(F, 1, lat, .3, D, .1), V(U, 1, B, .75, lat, .05), sec=U))
BEHIND_HEAD = sym(lambda lat: limb(V(lat, 1, U, .55, B, .15), V(-lat, 1, B, .45, U, .35), sec=U))
BACK_SQUAT = sym(lambda lat: limb(V(lat, .4, D, .35, B), V(U, .6, -lat, .2, B), sec=F))
PRAYER = sym(lambda lat: limb(V(D, .6, lat, .4, F), V(-lat, .9, F, .5, U), sec=F))
PRAYER_UP = sym(lambda lat: limb(V(U, .45, lat), V(U, .5, -lat), sec=F))
CACTUS = sym(lambda lat: limb(lat, U, sec=F))


def arms_alt(a, b):
    """Left arm as a, right arm as b."""
    return {**{k: v for k, v in a.items() if k.endswith(".L")}, **{k: v for k, v in b.items() if k.endswith(".R")}}


# --- Stances: P keyword dicts ---
def stand(**kw):
    return merge({"aim": dict(DOWN)}, kw)


def half_squat(depth=0.5, **kw):
    p = 30 * depth
    return merge({"pelvis": (p, 0, 0), "spine": (-p * 0.4, 0, 0), "legs": lw(p, 80 * depth, -35 * depth)}, kw)


def squat_pose(depth=1.0, wide=False, **kw):
    p = 40 * depth
    legs = lw(p, 85 * depth, -35 * depth)
    if wide:
        legs.update({"abd": 25, "rot": 35})
    return merge({"pelvis": (p, 0, 0), "spine": (-12 * depth, 0, 0), "legs": legs}, kw)


def hinge(angle=75, knee=15, **kw):
    """Hips back, trunk forward by angle, knees softly bent, shins vertical."""
    return merge({"pelvis": (angle, 0, 0), "legs": lw(angle, knee, 0)}, kw)


def split_stance(front="L", depth=0.0, **kw):
    """Feet apart front to back; depth 1 is a lunge with the back knee near the floor."""
    f = lw(0, 15 + 75 * depth, -5 * depth)
    b = lw(0, -15 - 10 * depth, -15 - 85 * depth, ankle=35 + 30 * depth)
    legs = {"LL": f, "RL": b} if front == "L" else {"LL": b, "RL": f}
    back = "R" if front == "L" else "L"
    c = (f"feet.{front}", f"knees.{back}" if depth > 0.8 else f"feet.{back}", "x", 15)
    return merge(legs, {"contacts": c}, kw)


def single_leg(free="R", knee=90, thigh=0, **kw):
    """Standing on one leg; the free leg's thigh forward by thigh, its knee bent."""
    return merge({("RL" if free == "R" else "LL"): lw(0, thigh, thigh - knee)}, kw)


def chair(**kw):
    """Sitting on an unseen chair or wheelchair."""
    return merge({"legs": lw(0, 90, 0)}, kw)


def floor_sit(**kw):
    """Sitting on the floor, legs long."""
    return merge({"legs": {"flex": 90, "ankle": -15}}, kw)


def kneel(**kw):
    """Kneeling upright, shins and feet flat."""
    return merge({"aim": {"leg_upper.L": (D, F), "leg_upper.R": (D, F), "leg_lower.L": (B, D), "leg_lower.R": (B, D),
                          "foot.L": (B, D), "foot.R": (B, D)}}, kw)


def half_kneel(front="L", **kw):
    back = "R" if front == "L" else "L"
    return merge({("LL" if front == "L" else "RL"): lw(0, 90, 0),
                  "aim": {f"leg_upper.{back}": (D, F), f"leg_lower.{back}": (B, D), f"foot.{back}": (B, D)},
                  "contacts": (f"feet.{front}", f"knees.{back}")}, kw)


HOOK = {"leg_upper": (V(B, 1.2, U), U), "leg_lower": (V(D, .35, B), B), "foot": (B, U)}  # supine, feet flat
TABLETOP = {"leg_upper": (U, F), "leg_lower": (B, U), "foot": (V(B, .3, U), U)}  # supine, shins level


def supine(**kw):
    return merge({"orient": "supine", "aim": {k + s: v for k, v in HOOK.items() for s in (".L", ".R")}}, kw)


def legs_both(d):
    return {k + s: v for k, v in d.items() for s in (".L", ".R")}


def Pk(**kw):
    """P from a keyword dict that may carry orient."""
    orient = kw.pop("orient", "standing")
    return P(orient, **kw)


# --- Flags read from a name ---
class C:
    def __init__(self, cat, name):
        self.cat, self.name = cat, name
        n = name
        self.wheel = "wheelchair" in n
        self.seated = self.wheel or bool(re.search(r"seated|sitting|_sit_|^sit_", n)) and "sit_up" not in n
        self.half_kneel = "half_kneeling" in n
        self.kneel = "kneeling" in n and not self.half_kneel
        self.single_leg = bool(re.search(r"single_leg|one_leg|one_legged", n))
        self.single_arm = bool(re.search(r"single_arm|one_arm|one_hand", n))
        self.alt = bool(re.search(r"alternating|alternate|switch", n))
        self.hold = bool(re.search(r"isometric|static|hold|_holds", n))
        self.wide = "wide" in n or "sumo" in n
        self.close = "close" in n or "diamond" in n or "narrow" in n
        self.jump = bool(re.search(r"jump|plyo|explosive|hop|leap", n))
        self.overhead = "overhead" in n
        self.lying = bool(re.search(r"lying|supine|floor_press|on_floor", n))


def base_for(c, default=None):
    """The stance an arm movement is shown in."""
    if c.seated:
        return chair()
    if c.half_kneel:
        return half_kneel()
    if c.kneel:
        return kneel()
    if c.single_leg:
        return single_leg()
    if "split" in c.name or "staggered" in c.name:
        return split_stance("L", 0.2)
    return default if default is not None else {}


def keys_alt(c, rest, a, alt_fn=None):
    """rest → a, or with alternating arms/legs rest → a → rest → mirror(a)."""
    if c.alt or c.single_arm or c.single_leg:
        if c.alt:
            return [rest, a, rest, mirror(a)]
        return [rest, a]
    return [rest, a]


def hold_or(c, keys):
    return [keys[-1]] if c.hold else keys


# ===================================================================== families
def K(*kws):
    """One key pose from merged keyword dicts."""
    return Pk(**merge(*kws))


def lifted(spec, m=0.35):
    """The key pose raised m metres off the floor (hanging from a bar, in the air)."""
    out = dict(spec)
    out["_lift"] = m
    return out


def lift_all(keys, m=0.35):
    return [lifted(k, m) for k in keys]


def two(c, rest, act):
    """rest → act; alternating: rest → act → rest → mirrored act; a hold shows act only."""
    if c.hold:
        return [act]
    if c.alt:
        return [rest, act, rest, mirror(act)]
    return [rest, act]


def one_arm(c, act_both, rest_both):
    """For single-arm moves: the left arm acts, the right arm stays at rest."""
    if c.single_arm:
        return arms_alt(act_both, rest_both)
    return act_both


def side_arms(c, act, rest):
    """Arms for a move: both, one (single arm) or alternating (left first)."""
    if c.single_arm or c.alt:
        return arms_alt(act, rest)
    return act


# --- Arms, standing (or seated/kneeling by name) ---
CURL_UP = sym(lambda lat: {"arm_upper": (V(D, .1, lat), F), "arm_lower": (V(U, .75, F), B), "hand": (V(U, .5, F), V(-lat))})
DOWN_ELBOW = sym(lambda lat: {"arm_upper": (V(D, .1, lat), F), "arm_lower": (V(D, .9, F, .2), F), "hand": (V(D, .9, F, .2), F)})


def f_curl(c):
    b = base_for(c, stand())
    rest = K(b, {"aim": DOWN_ELBOW})
    act = K(b, {"aim": side_arms(c, CURL_UP, DOWN_ELBOW)})
    return two(c, rest, act)


def f_wrist_curl(c):
    arm = lambda h: sym(lambda lat: {"arm_upper": (V(D, .15, lat), F), "arm_lower": (F, U), "hand": (h, U)})
    return [K(chair(), {"aim": arm(V(F, 1, D, .6))}), K(chair(), {"aim": arm(V(F, 1, U, .6))})]


def f_press(c, dip=False, split=False):
    b = base_for(c, stand())
    if split:
        b = split_stance("L", 0.15)
    s0 = merge(b, half_squat(0.3)) if dip else b
    if c.single_arm:
        return [K(s0, {"aim": arms_alt(AT_SHOULDERS, DOWN)}), K(b, {"aim": arms_alt(OVERHEAD, DOWN)})]
    rest, act = K(s0, {"aim": AT_SHOULDERS}), K(b, {"aim": side_arms(c, OVERHEAD, AT_SHOULDERS)})
    return two(c, rest, act)


def f_raise(c, kind="lateral"):
    b = base_for(c, stand())
    up = {"lateral": T_ARMS, "front": FORWARD, "v": sym(lambda lat: limb(V(F, 1, lat, .8, U, .2), sec=U)),
          "diagonal": sym(lambda lat: limb(V(U, .6, lat, .8, F, .3), sec=F)),
          "scaption": sym(lambda lat: limb(V(lat, 1, F, .6, U, .1), sec=U))}[kind]
    return two(c, K(b, {"aim": DOWN}), K(b, {"aim": side_arms(c, up, DOWN)}))


def f_rear_raise(c):
    b = base_for(c, hinge(70, 20))
    if c.seated:
        b = merge(chair(), {"pelvis": (60, 0, 0), "legs": lw(60, 90, 0)})
    if c.lying:
        b = {"orient": "prone"}
    hang = sym(lambda lat: limb(V(D, 1, lat, .1), sec=F))
    out = sym(lambda lat: limb(V(lat, 1, D, .25), sec=D))
    return two(c, K(b, {"aim": hang}), K(b, {"aim": side_arms(c, out, hang)}))


UPRIGHT_TOP = sym(lambda lat: {"arm_upper": (V(lat, 1, U, .15, F, .1), F), "arm_lower": (V(D, .5, -lat, .8, F, .25), F),
                               "hand": (V(D, .8, -lat, .3), F)})


def f_upright_row(c):
    b = base_for(c, stand())
    return [K(b, {"aim": DOWN}), K(b, {"aim": UPRIGHT_TOP})]


def f_shrug(c):
    b = base_for(c, stand())
    return [K(b, {"aim": DOWN, "neck": (-5, 0, 0)})]


def f_triceps_overhead(c):
    b = base_for(c, stand())
    bent = sym(lambda lat: {"arm_upper": (V(U, 1, lat, .15, F, .1), V(-lat)), "arm_lower": (V(D, 1, B, .3, -lat, .2), F),
                            "hand": (V(D, 1, B, .3), F)})
    up = sym(lambda lat: limb(V(U, 1, lat, .1), sec=F))
    if c.single_arm:
        return [K(b, {"aim": arms_alt(bent, DOWN)}), K(b, {"aim": arms_alt(up, DOWN)})]
    return two(c, K(b, {"aim": bent}), K(b, {"aim": side_arms(c, up, bent)}))


def f_pressdown(c):
    b = base_for(c, stand())
    bent = sym(lambda lat: {"arm_upper": (V(D, 1, F, .15, lat, .1), F), "arm_lower": (V(F, 1, D, .3), U), "hand": (V(F, 1, D, .4), U)})
    straight = sym(lambda lat: limb(V(D, 1, F, .15, lat, .1), sec=F))
    return two(c, K(b, {"aim": bent}), K(b, {"aim": straight}))


def f_kickback(c):
    b = base_for(c, hinge(70, 25))
    bent = sym(lambda lat: {"arm_upper": (V(B, 1, U, .15, lat, .1), D), "arm_lower": (D, B), "hand": (D, B)})
    straight = sym(lambda lat: limb(V(B, 1, U, .2, lat, .1), sec=D))
    return two(c, K(b, {"aim": bent}), K(b, {"aim": side_arms(c, straight, bent)}))


def f_dip(c, bench=False):
    if bench:
        b = {"legs": lw(0, 85, 85, ankle=-10)}
        low = sym(lambda lat: {"arm_upper": (V(B, 1, U, .35, lat, .15), D), "arm_lower": (V(D, 1, B, .1), B), "hand": (B, D)})
        high = sym(lambda lat: limb(V(D, 1, B, .25, lat, .1), sec=B, hsec=D))
    else:
        b = {"legs": lw(0, -5, -70)}
        low = sym(lambda lat: {"arm_upper": (V(B, 1, D, .2, lat, .2), D), "arm_lower": (V(D, 1, lat, .1), B), "hand": (F, D)})
        high = sym(lambda lat: limb(V(D, 1, lat, .15), sec=B))
    if bench:
        return hold_or(c, [K(b, {"aim": low}), K(b, {"aim": high})])
    return hold_or(c, [lifted(K(b, {"aim": low}), .25), lifted(K(b, {"aim": high}), .5)])


# --- Arms, lying on the back ---
def lie(c):
    return merge(supine(), {"aim": {}})


def f_bench_press(c, close=False, floor=False):
    b = lie(c)
    low = sym(lambda lat: {"arm_upper": (V(B, .6, D, .45, lat, .3) if close else V(lat, 1, D, .45, B, .2), B),
                           "arm_lower": (U, B), "hand": (U, V(-lat))})
    up = sym(lambda lat: limb(V(U, 1, lat, .05), sec=B, hsec=V(-lat)))
    if c.single_arm:
        return [K(b, {"aim": arms_alt(low, up)}), K(b, {"aim": arms_alt(up, up)})]
    return two(c, K(b, {"aim": low}), K(b, {"aim": side_arms(c, up, low)}))


def f_flye(c, standing=False):
    if standing:
        b = base_for(c, split_stance("L", 0.1))
        return [K(b, {"aim": sym(lambda lat: limb(V(lat, 1, F, .2), sec=F))}),
                K(b, {"aim": sym(lambda lat: limb(V(F, 1, D, .5, -lat, .2), sec=V(-lat)))})]
    b = lie(c)
    wide = sym(lambda lat: limb(V(lat, 1, U, .2), sec=U))
    up = sym(lambda lat: limb(V(U, 1, lat, .1), sec=V(-lat)))
    return [K(b, {"aim": wide}), K(b, {"aim": up})]


def f_skull_crusher(c):
    b = lie(c)
    bent = sym(lambda lat: {"arm_upper": (V(U, 1, F, .25, lat, .05), F), "arm_lower": (V(F, 1, U, .4), U), "hand": (F, U)})
    up = sym(lambda lat: limb(V(U, 1, lat, .05), sec=F))
    return two(c, K(b, {"aim": bent}), K(b, {"aim": side_arms(c, up, bent)}))


def f_pullover(c):
    b = lie(c)
    over = sym(lambda lat: limb(V(F, 1, D, .1, lat, .1), sec=U))
    up = sym(lambda lat: limb(V(U, 1, F, .1), sec=F))
    return [K(b, {"aim": over}), K(b, {"aim": up})]


# --- Pressing and pulling, standing ---
def f_chest_press_standing(c):
    b = base_for(c, split_stance("L", 0.15))
    low = sym(lambda lat: {"arm_upper": (V(lat, 1, B, .7, D, .3), F), "arm_lower": (F, U), "hand": (F, V(-lat))})
    return two(c, K(b, {"aim": low}), K(b, {"aim": side_arms(c, FORWARD, low)}))


ROW_TOP = sym(lambda lat: {"arm_upper": (V(U, .8, B, .6, lat, .25), D), "arm_lower": (V(D, 1, B, .1), F), "hand": (D, F)})
HANG = sym(lambda lat: limb(V(D, 1, lat, .05), sec=F))


def f_row_bent(c):
    b = base_for(c, hinge(65, 25))
    if c.single_leg:
        b = {"pelvis": (80, 0, 0), "LL": lw(80, 15, 0), "RL": {"flex": 5, "knee": 5, "ankle": 30}}
    if c.seated:
        b = merge(chair(), {"pelvis": (55, 0, 0), "legs": lw(55, 90, 0)})
    return two(c, K(b, {"aim": HANG}), K(b, {"aim": side_arms(c, ROW_TOP, HANG)}))


def f_row_seated(c, standing=False):
    if standing:
        b0 = b1 = split_stance("L", 0.2)
    else:
        legs = {"legs": {"flex": 100, "knee": 45, "ankle": -20}}
        b0, b1 = merge(legs, {"pelvis": (15, 0, 0)}), merge(legs, {"pelvis": (-5, 0, 0)})
    reach = sym(lambda lat: limb(F, sec=U, hsec=U))
    pull = sym(lambda lat: {"arm_upper": (V(B, 1, D, .5, lat, .25), F), "arm_lower": (F, U), "hand": (F, U)})
    return two(c, K(b0, {"aim": reach}), K(b1, {"aim": side_arms(c, pull, reach)}))


def f_renegade_row(c):
    row = one("L", {"arm_upper": (V(U, .8, B, .5, LT, .2), D), "arm_lower": (D, F), "hand": (D, F)})
    return [plank_high(), plank_high(aim=row), plank_high(), mirror(plank_high(aim=row))]


def f_inverted_row(c):
    orient = (V(F, 1, U, .55), V(U, 1, B, .55))
    hang = sym(lambda lat: limb(V(U, 1, lat, .1), sec=F))
    pull = sym(lambda lat: {"arm_upper": (V(lat, .7, D, .5, B, .3), F), "arm_lower": (U, F), "hand": (U, F)})
    return two(c, P(orient, aim=hang), P(orient, aim=side_arms(c, pull, hang)))


def f_face_pull(c):
    b = base_for(c, stand())
    reach = sym(lambda lat: limb(V(F, 1, U, .15), sec=U))
    pull = sym(lambda lat: {"arm_upper": (V(lat, 1, B, .3, U, .1), F), "arm_lower": (V(U, 1, F, .3), B), "hand": (U, B)})
    return [K(b, {"aim": reach}), K(b, {"aim": pull})]


def f_pull_apart(c):
    b = base_for(c, stand())
    return [K(b, {"aim": FORWARD}), K(b, {"aim": T_ARMS})]


def f_pulldown(c, straight=False):
    b = base_for(c, kneel() if c.kneel else chair())
    if straight:
        b = base_for(c, hinge(25, 15))
        return [K(b, {"aim": sym(lambda lat: limb(V(F, 1, U, .7), sec=U))}), K(b, {"aim": sym(lambda lat: limb(V(D, 1, F, .2), sec=F))})]
    up = sym(lambda lat: limb(V(U, 1, lat, .35), sec=F))
    down = sym(lambda lat: {"arm_upper": (V(lat, .8, D, .6, B, .1), F), "arm_lower": (V(U, 1, -lat, .1), F), "hand": (U, F)})
    return two(c, K(b, {"aim": up}), K(b, {"aim": side_arms(c, down, up)}))


HANG_UP = sym(lambda lat: limb(V(U, 1, lat, .25), sec=F))
PULLED = sym(lambda lat: {"arm_upper": (V(lat, .75, D, .6, F, .1), F), "arm_lower": (V(U, 1, -lat, .1), F), "hand": (U, F)})


def f_pull_up(c, legs90=False, muscle=False):
    b = {"legs": {"flex": 90} if legs90 else {"flex": 10, "knee": 25}}
    keys = [lifted(K(b, {"aim": HANG_UP}), .3), lifted(K(b, {"aim": PULLED}), .55)]
    if muscle:
        keys.append(lifted(K(b, {"aim": sym(lambda lat: limb(V(D, 1, lat, .15), sec=B))}), .9))
    return hold_or(c, keys)


def f_rope_climb(c):
    a = arms_alt(sym(lambda lat: limb(V(U, 1, lat, .05), sec=F)), sym(lambda lat: {"arm_upper": (V(U, .5, lat, .5, F, .3), F), "arm_lower": (U, F), "hand": (U, F)}))
    k = lifted(P(aim=a, RL={"flex": 60, "knee": 80}), .5)
    return [k, mirror(k)]


def f_ext_rotation(c, at90=False, internal=False):
    b = base_for(c, stand())
    if c.lying:
        b = {"orient": "side_left"}
    if at90:
        a0 = sym(lambda lat: {"arm_upper": (lat, F), "arm_lower": (F, U), "hand": (F, U)})
        a1 = sym(lambda lat: {"arm_upper": (lat, F), "arm_lower": (U, B), "hand": (U, B)})
    else:
        a0 = sym(lambda lat: {"arm_upper": (V(D, 1, lat, .1), F), "arm_lower": (F, U), "hand": (F, U)})
        a1 = sym(lambda lat: {"arm_upper": (V(D, 1, lat, .1), F), "arm_lower": (V(lat, 1, F, .35), U), "hand": (V(lat, 1, F, .35), U)})
        if internal:
            a0, a1 = a1, sym(lambda lat: {"arm_upper": (V(D, 1, lat, .1), F), "arm_lower": (V(F, .5, -lat, 1), U), "hand": (V(-lat), U)})
    return [K(b, {"aim": side_arms(c, a0, a0)}), K(b, {"aim": side_arms(c, a1, a0)})]


def f_ity(c, kind="i"):
    """Prone raises: arms in an I, T, Y, W or L, lifted off the floor."""
    shapes = {"i": lambda lat, up: limb(V(F, 1, U, up), sec=V(-lat)),
              "t": lambda lat, up: limb(V(lat, 1, U, up), sec=F),
              "y": lambda lat, up: limb(V(F, 1, lat, .75, U, up), sec=U),
              "w": lambda lat, up: {"arm_upper": (V(lat, .7, B, .5, U, up), F), "arm_lower": (V(F, 1, lat, .3, U, up), D), "hand": (F, D)},
              "l": lambda lat, up: {"arm_upper": (V(lat, 1, U, up), F), "arm_lower": (V(F, 1, U, up), D), "hand": (F, D)}}
    s = shapes[kind]
    lo = P("prone", aim=sym(lambda lat: s(lat, 0.0)))
    hi = P("prone", spine=(-8, 0, 0), aim=sym(lambda lat: s(lat, 0.35)))
    return [lo, hi]


def f_arm_circles(c):
    return list(USER["lateral_raise/32"][1])


def f_around_world(c):
    b = base_for(c, stand())
    return [K(b, {"aim": sym(lambda lat: limb(V(F, 1, D, .6), sec=U))}), K(b, {"aim": OVERHEAD}),
            K(b, {"aim": sym(lambda lat: limb(V(lat, 1, U, .3), sec=F))})]


def f_shoulder_ext(c):
    b = base_for(c, stand())
    return [K(b, {"aim": DOWN}), K(b, {"aim": sym(lambda lat: limb(V(D, 1, B, .7, lat, .1), sec=F))})]


def f_wall_slide(c):
    b = base_for(c, stand())
    return [K(b, {"aim": CACTUS}), K(b, {"aim": sym(lambda lat: limb(V(U, 1, lat, .45), sec=F))})]


def f_punch(c, direction="forward", both=False):
    b = base_for(c, split_stance("L", 0.15))
    d = {"forward": F, "up": V(U, 1, F, .3), "down": V(D, 1, F, .5), "sideways": None}[direction]
    ext = (lambda lat: limb(lat if d is None else d, sec=U))
    act = sym(ext) if both else arms_alt(sym(ext), GUARD)
    k0, k1 = K(b, {"aim": GUARD}), K(b, {"aim": act})
    return [k0, k1] if both else [k0, k1, k0, mirror(k1)]


# --- Lower body ---
def squat_arms(c):
    n = c.name
    if re.search(r"overhead|snatch|kbs_overhead", n):
        return OVERHEAD
    if re.search(r"goblet|kettlebell|medicine|plate|sandbag|bear_hug|ball|wall_ball", n):
        return CHEST
    if re.search(r"front|zercher|crossed_arm|clean", n):
        return FRONT_RACK
    if re.search(r"prisoner|waist_slimming", n):
        return BEHIND_HEAD
    if re.search(r"barbell|back_squat|body_bar|smith|hack|safety|band", n):
        return BACK_SQUAT
    if re.search(r"dumbbell|suitcase", n):
        return DOWN
    return FORWARD


def f_squat(c, wide=None, jump=None, press=False, hold=None):
    wide = c.wide or bool(re.search(r"sumo|plie|wide", c.name)) if wide is None else wide
    a = squat_arms(c)
    st = {"legs": {"abd": 20, "rot": 30}} if wide else {}
    top = K(stand(), st, {"aim": OVERHEAD if press else a})
    bottom = K(squat_pose(1.0, wide=wide), {"aim": FRONT_RACK if press else a})
    if hold if hold is not None else c.hold:
        return [bottom]
    keys = [top, bottom]
    if press:
        keys = [bottom, top]
    if jump if jump is not None else c.jump:
        air = lifted(P(legs={"ankle": 45, "flex": 5}, aim=OVERHEAD), .25)
        keys = [bottom, air]
    return keys


def f_pistol(c):
    sq = {"LL": lw(45, 85, -40), "RL": {"flex": 45 + 85, "knee": 5, "ankle": 0}, "pelvis": (45, 0, 0), "spine": (-15, 0, 0)}
    top = P(RL={"flex": 70, "knee": 5}, aim=FORWARD)
    return [top, K(sq, {"aim": FORWARD})]


def f_step_up(c):
    a = squat_arms(c) if re.search(r"barbell", c.name) else DOWN
    k0 = P(LL=lw(0, 70, 5), aim=a, contacts=("feet.L", "feet.R", "x", 15))
    k1 = P(RL=lw(0, 75, -10), aim=a)
    return [k0, k1] if not c.alt else [k0, k1, mirror(k0), mirror(k1)]


def lunge_arms(c):
    n = c.name
    if re.search(r"overhead", n):
        return OVERHEAD
    if re.search(r"reach|arm_reach", n):
        return OVERHEAD
    if re.search(r"barbell", n):
        return BACK_SQUAT
    if re.search(r"medicine|kettlebell|goblet|plate", n):
        return CHEST
    if re.search(r"dumbbell|walking|farmers|offset", n):
        return DOWN
    return HANDS_HIPS


def f_lunge(c, kind="forward"):
    n = c.name
    a = lunge_arms(c)
    extra = {}
    if re.search(r"rotation|twist", n):
        extra = {"spine": (0, 0, 40)}
        a = sym(lambda lat: limb(V(F, 1, D, .2), sec=U))
    if "side_bend" in n:
        extra = {"spine": (0, 25, 0)}
        a = arms_alt(DOWN, OVERHEAD)
    top = K(stand(), {"aim": a})
    if kind == "side":
        k = P(pelvis=(35, 0, 0), spine=(-10, 0, 0), aim={
            "leg_upper.L": (V(LT, 1, F, .45, D, .55), U), "leg_lower.L": (V(D, 1, LT, .1), LT), "foot.L": (V(F, 1, LT, .5), U),
            "leg_upper.R": (V(RT, 1, D, .8), F), "leg_lower.R": (V(RT, 1, D, .8), F), "foot.R": (V(F, 1, RT, .2), U),
            **(CHEST if a is not DOWN else DOWN)}, contacts=("feet.L", "feet.R", "y", 15))
        return [top, k, top, mirror(k)] if (c.alt or "to_side" in n or "shifting" in n) else [top, k]
    if kind == "curtsy":
        k = K(split_stance("L", 0.9), {"RL": {"abd": -25}, "aim": a})
        return [top, k, top, mirror(k)]
    if kind == "bulgarian":
        rear = {"leg_upper.R": (V(D, 1, B, .5), F), "leg_lower.R": (V(B, 1, U, .1), D), "foot.R": (B, D)}
        k0 = P(LL=lw(0, 30, 0), aim={**a, **{"leg_upper.R": (V(D, 1, B, .2), F), "leg_lower.R": (V(B, 1, D, .2), D), "foot.R": (B, D)}})
        k1 = P(LL=lw(0, 85, -10), pelvis=(10, 0, 0), aim={**a, **rear})
        return [k0, k1]
    if kind == "split":
        return hold_or(c, [K(split_stance("L", 0.15), {"aim": a}), K(split_stance("L", 1.0), extra, {"aim": a})])
    if kind == "jump":
        k = K(split_stance("L", 0.9), {"aim": a})
        air = lifted(P(LL={"flex": 40, "knee": 40}, RL={"flex": -20, "knee": 60}, aim=a), .3)
        return [k, air, mirror(k), mirror(air)]
    k = K(split_stance("L", 0.95), extra, {"aim": a})
    if "knee" in n and ("high_knee" in n or "knee_lift" in n or "balance" in n):
        return [k, P(LL=lw(0, 90, 0), aim=a)]
    if c.alt or "walking" in n or "alternating" in n:
        return [top, k, top, mirror(k)]
    return hold_or(c, [top, k])


def f_deadlift(c, kind="conventional"):
    n = c.name
    wide = kind == "sumo"
    hang = sym(lambda lat: limb(V(D, 1, lat, .25 if wide else .05), sec=F))
    if kind in ("rdl", "stiff"):
        bottom = K(hinge(85, 15), {"aim": hang})
    elif kind == "rack":
        bottom = K(hinge(45, 25), {"aim": hang})
    elif kind == "single":
        bottom = P(pelvis=(85, 0, 0), LL=lw(85, 12, 0), RL={"flex": 5, "knee": 5, "ankle": 30}, aim=hang)
        top = P(RL=lw(0, -10, -40), aim=DOWN)
        return [top, bottom]
    else:
        legs = lw(55, 65, -15)
        if wide:
            legs.update({"abd": 25, "rot": 35})
        bottom = P(pelvis=(55, 0, 0), spine=(-5, 0, 0), legs=legs, aim=hang)
    top = P(legs={"abd": 20, "rot": 30} if wide else {}, aim=DOWN)
    if "high_pull" in n:
        top = P(legs={"ankle": 30, **({"abd": 20, "rot": 30} if wide else {})}, aim=UPRIGHT_TOP)
    if "row" in n:
        return [bottom, top, K(hinge(65, 25), {"aim": ROW_TOP})]
    return [bottom, top]


def f_good_morning(c):
    a = BEHIND_HEAD if not re.search(r"barbell|bar_|band", c.name) else BACK_SQUAT
    if c.seated:
        return [K(chair(), {"aim": a}), K(chair(), {"pelvis": (60, 0, 0), "legs": lw(60, 90, 0), "aim": a})]
    if c.single_leg:
        return [P(RL=lw(0, -10, -40), aim=a), P(pelvis=(85, 0, 0), LL=lw(85, 12, 0), RL={"flex": 5}, aim=a)]
    b = split_stance("L", 0.15) if re.search(r"split|staggered", c.name) else {}
    return [K(b, {"aim": a}), K(b, hinge(80, 15), {"aim": a})]


def f_bridge(c, single=False, march=False):
    lo, hi = bridge(0.3), bridge(1.0)
    if c.hold:
        return [hi]
    if single:
        ext = {"leg_upper.R": (V(B, 1, U, .2), U), "leg_lower.R": (V(B, 1, U, .2), U), "foot.R": (V(B, 1, U, .6), U)}
        lo = P("supine", pelvis=(-12, 0, 0), aim={**{k: v for k, v in hi.items()}, **{}}) if False else lo
        lo2 = dict(bridge(0.3)); lo2.update({k: v for k, v in ext.items()})
        hi2 = dict(bridge(1.0)); hi2.update({k: v for k, v in ext.items()})
        return [lo2, hi2]
    if march:
        hm = dict(bridge(1.0)); hm.update({"leg_upper.R": (V(U, 1, F, .15), F), "leg_lower.R": (B, U), "foot.R": (V(B, 1, U, .3), U)})
        return [hi, hm, hi, mirror(hm)]
    return [lo, hi]


def f_kb_swing(c, overhead=False):
    b0 = hinge(70, 25)
    between = sym(lambda lat: limb(V(D, 1, B, .55, lat, .05), sec=F))
    a1 = OVERHEAD if overhead else sym(lambda lat: limb(V(F, 1, D, .05), sec=U))
    if c.single_arm:
        between, a1 = arms_alt(between, DOWN), arms_alt(a1, DOWN)
    return [K(b0, {"aim": between}), P(aim=a1)]


def f_calf_raise(c):
    n = c.name
    if c.seated:
        return [K(chair(), {"aim": HANDS_HIPS}), K(chair(), {"legs": {**lw(0, 90, 0), "ankle": 35}, "aim": HANDS_HIPS})]
    b = {}
    if "donkey" in n:
        b = hinge(85, 5)
    free = {"RL": lw(85 if "donkey" in n else 0, -5, -90)} if c.single_leg else {}
    a = DOWN if re.search(r"dumbbell|barbell|weighted", n) else HANDS_HIPS
    up_legs = {"legs": {"ankle": 35}} if not c.single_leg else {"LL": {"ankle": 35}}
    return [K(b, free, {"aim": a}), K(b, free, up_legs, {"aim": a})]


def f_leg_curl(c):
    n = c.name
    if "sliding" in n or "swiss_ball" in n:
        straight = P("supine", pelvis=(-15, 0, 0), aim={"arm_upper": (B, U), "arm_lower": (B, U), "hand": (B, "medial")}, contacts=("feet", "back"))
        return [straight, bridge(1.0)]
    if "standing" in n or (c.single_leg and "prone" not in n and "lying" not in n and "seated" not in n):
        return [P(aim=HANDS_HIPS), P(RL=lw(0, -5, -110), aim=HANDS_HIPS)]
    if c.seated:
        return [K(chair(), {"legs": lw(0, 90, 90, ankle=0)}), K(chair(), {"legs": lw(0, 90, -15)})]
    return [P("prone", aim=FOREARM), P("prone", legs={"knee": 100}, aim=FOREARM)]


def f_leg_ext(c):
    return [K(chair(), {"aim": HANDS_HIPS}), K(chair(), {"legs": lw(0, 90, 90, ankle=0), "aim": HANDS_HIPS})]


def f_hip_abd_standing(c, kind="abd"):
    leg_ = {"abd": {"abd": 35}, "add": {"abd": -20, "flex": 10}, "rear": {"flex": -30}, "front": {"flex": 45}}[kind]
    return [P(aim=HANDS_HIPS), P(RL=leg_, aim=HANDS_HIPS)]


def f_side_lying_leg(c, kind="abd"):
    base = {"aim": {"arm_upper.L": (F, RT), "arm_lower.L": (V(U, 1, RT, .2), RT), "hand.L": (U, RT),
                    "arm_upper.R": (V(B, 1, D, .3), RT), "arm_lower.R": (V(B, 1, D, .3), RT), "hand.R": (B, RT)}}
    if kind == "clam":
        legs = {"legs": {"flex": 45, "knee": 90}}
        return [K({"orient": "side_left"}, base, legs), K({"orient": "side_left"}, base, legs, {"RL": {"flex": 45, "knee": 90, "rot": 40, "abd": 25}})]
    if kind == "inner":
        return [K({"orient": "side_left"}, base, {"RL": {"flex": 70, "knee": 90}}),
                K({"orient": "side_left"}, base, {"RL": {"flex": 70, "knee": 90}, "LL": {"abd": -25}})]
    return [K({"orient": "side_left"}, base), K({"orient": "side_left"}, base, {"RL": {"abd": 40}})]


def f_quad_leg(c, kind="hydrant"):
    if kind == "hydrant":
        lift = {"leg_upper.R": (V(RT, 1, D, .35), F), "leg_lower.R": (V(B, 1, RT, .1), D), "foot.R": (B, D)}
    elif kind == "donkey":
        lift = {"leg_upper.R": (V(B, 1, U, .35), D), "leg_lower.R": (V(U, 1, B, .1), B), "foot.R": (B, U)}
    else:  # extension
        lift = reach_leg("R")
    k0, k1 = quad(), quad(aim=lift)
    return [k0, k1, k0, mirror(k1)] if c.alt else [k0, k1]


def f_lateral_walk(c):
    b = half_squat(0.4)
    k0 = K(b, {"aim": HANDS_HIPS})
    k1 = K(b, {"LL": {"abd": 18}, "RL": {"abd": 8}, "aim": HANDS_HIPS})
    return [k0, k1]


def f_hip_circles(c):
    return [P(RL=lw(0, 90, 0), aim=HANDS_HIPS), P(RL={"flex": 60, "abd": 60, "rot": 40, "knee": 90}, aim=HANDS_HIPS)]


# --- Core ---
SUP_ARMS_FLOOR = {"arm_upper": (B, U), "arm_lower": (B, U), "hand": (B, "medial")}
CROSSED = sym(lambda lat: {"arm_upper": (V(B, .6, -lat, .2, U, .3), U), "arm_lower": (V(-lat, 1, F, .4, U, .2), U), "hand": (V(-lat, 1, F, .3), U)})
REACH_KNEES = sym(lambda lat: limb(V(B, 1, U, .55), sec=U, hsec=V(-lat)))


def crunch_arms(c):
    n = c.name
    if re.search(r"crossed|cross_arm", n):
        return CROSSED
    if re.search(r"reach|plate|ball|medicine|curl_up", n):
        return REACH_KNEES
    return behind_head_aim()


def f_crunch(c, legs="hook", twist=0):
    a = crunch_arms(c)
    lg = {"hook": legs_both(HOOK), "table": legs_both(TABLETOP), "straight": {}, "up": {}}[legs]
    extra = {"legs": {"flex": 90, "ankle": 30}} if legs == "up" else {}
    flat = P("supine", aim={**lg, **a}, **extra)
    curl = P("supine", spine=(35, 0, twist), neck=(15, 0, 0), aim={**lg, **a}, **extra)
    if twist:
        return [flat, curl, flat, mirror(curl)]
    return hold_or(c, [flat, curl])


def f_reverse_crunch(c):
    tab = P("supine", aim={**legs_both(TABLETOP), **SUP_ARMS_FLOOR})
    curled = P("supine", pelvis=(-20, 0, 0), spine=(20, 0, 0),
               aim={**legs_both({"leg_upper": (V(U, 1, F, .9), F), "leg_lower": (V(B, 1, U, .6), U), "foot": (V(B, 1, U, .3), U)}),
                    **SUP_ARMS_FLOOR})
    return [tab, curled]


def f_bicycle(c):
    return list(USER["core/53"][1])


def f_flutter(c):
    k = P("supine", LL={"flex": 30, "ankle": 30}, RL={"flex": 10, "ankle": 30}, aim=SUP_ARMS_FLOOR)
    return [k, mirror(k)]


def f_hollow(c, rock=False):
    k = P("supine", spine=(25, 0, 0), neck=(10, 0, 0), legs={"flex": 25, "ankle": 30},
          aim=sym(lambda lat: limb(V(F, 1, U, .45), sec=U)))
    if rock:
        return [k, P("supine", spine=(35, 0, 0), neck=(10, 0, 0), legs={"flex": 15, "ankle": 30}, aim=sym(lambda lat: limb(V(F, 1, U, .7), sec=U)))]
    return [k]


def f_leg_lower(c, top=90, bottom=20, single=False):
    k0 = P("supine", legs={"flex": top, "ankle": 20}, aim=SUP_ARMS_FLOOR)
    k1 = P("supine", legs={"flex": bottom, "ankle": 20}, aim=SUP_ARMS_FLOOR)
    if single:
        k0 = P("supine", LL={"flex": top, "ankle": 20}, aim=SUP_ARMS_FLOOR)
        k1 = P("supine", LL={"flex": bottom, "ankle": 20}, aim=SUP_ARMS_FLOOR)
        return [k0, k1, mirror(k0), mirror(k1)] if c.alt else [k0, k1]
    return [k1, k0]


def f_hanging_raise(c, knees=False, toes=False, twist=0):
    if knees:
        up = {"legs": {"flex": 100, "knee": 100}}
    elif toes:
        up = {"legs": {"flex": 140}, "pelvis": (-20, 0, 0)}
    else:
        up = {"legs": {"flex": 90}}
    if twist:
        up = merge(up, {"pelvis": (0, 0, twist)})
    keys = [K({"aim": HANG_UP}), K(up, {"aim": HANG_UP})]
    if c.single_leg:
        keys = [K({"aim": HANG_UP}), K({"LL": up["legs"]}, {"aim": HANG_UP})]
    return hold_or(c, [lifted(keys[0], .3), lifted(keys[1], .6)])


def f_situp(c, kind="situp"):
    a = CROSSED if "crossed" in c.name else (REACH_KNEES if re.search(r"medicine|reach|punch|plate", c.name) else behind_head_aim())
    flat = P("supine", aim={**legs_both(HOOK), **a})
    if kind == "situp":
        return hold_or(c, [flat, P("supine", pelvis=(70, 0, 0), spine=(15, 0, 0), aim={**legs_both(HOOK), **a})])
    if kind == "vup":
        flat = P("supine", aim=sym(lambda lat: limb(V(F, 1, D, .05), sec=U)))
        v = P("supine", pelvis=(50, 0, 0), aim={**legs_both({"leg_upper": (V(B, 1, U, 1.2), U), "leg_lower": (V(B, 1, U, 1.2), U),
                                                              "foot": (V(B, 1, U, 1.6), U)}),
                                              **sym(lambda lat: limb(V(B, 1, U, 1.0), sec=U))})
        if "bent_knee" in c.name or "tuck" in c.name:
            v = P("supine", pelvis=(50, 0, 0), aim={**legs_both({"leg_upper": (V(U, 1, B, .5), F), "leg_lower": (V(B, 1, U, .2), U),
                                                                  "foot": (B, U)}), **sym(lambda lat: limb(V(B, 1, U, .5), sec=U))})
        return [flat, v]
    if kind == "butterfly":
        bf = legs_both({"leg_upper": (V(U, .5, B, .6), F), "leg_lower": (V(B, .2, D, .3), U), "foot": (B, U)})
        lg = {"legs": {"flex": 50, "abd": 50, "rot": 60, "knee": 120}}
        return [P("supine", aim=sym(lambda lat: limb(V(F, 1, D, .05), sec=U)), **lg),
                P("supine", pelvis=(75, 0, 0), spine=(25, 0, 0), aim=REACH_KNEES, **lg)]


def f_vsit(c, arms="forward", tuck=False):
    lg = ({"leg_upper": (V(U, 1, B, .6), F), "leg_lower": (V(B, 1, U, .25), U), "foot": (B, U)} if tuck else
          {"leg_upper": (V(B, 1, U, 1.0), U), "leg_lower": (V(B, 1, U, 1.0), U), "foot": (V(B, 1, U, 1.3), U)})
    a = {"forward": sym(lambda lat: limb(V(B, 1, U, .1), sec=U, hsec=V(-lat))), "up": sym(lambda lat: limb(V(U, 1, F, .3), sec=F))}[arms]
    return [P("supine", pelvis=(50, 0, 0), aim={**legs_both(lg), **a})]


def f_russian_twist(c):
    lg = legs_both({"leg_upper": (V(B, 1, U, .9), U), "leg_lower": (V(B, 1, D, .15), U), "foot": (V(B, 1, U, .3), U)})
    k = P("supine", pelvis=(50, 0, 0), spine=(0, 0, 40), arms={"flex": 60, "elbow": 70}, aim=lg)
    return [k, mirror(k)]


def f_dead_bug(c):
    base = {**legs_both(TABLETOP), **sym(lambda lat: limb(U, sec=F))}
    ext = {**base, "arm_upper.L": (V(F, 1, U, .2), U), "arm_lower.L": (V(F, 1, U, .2), U), "hand.L": (F, U),
           "leg_upper.R": (V(B, 1, U, .25), U), "leg_lower.R": (V(B, 1, U, .25), U), "foot.R": (V(B, 1, U, .5), U)}
    k0, k1 = P("supine", aim=base), P("supine", aim=ext)
    return [k0, k1, k0, mirror(k1)]


def f_superman(c, alt=False, hands="forward"):
    a0 = {"forward": sym(lambda lat: limb(V(F, 1, lat, .15), sec=D)), "head": sym(lambda lat: {"arm_upper": (V(lat, 1, F, .3, U, .2), D), "arm_lower": (V(-lat, 1, F, .5, U, .3), D), "hand": (V(-lat, 1, F, .3), D)}),
          "sides": sym(lambda lat: limb(V(B, 1, lat, .1), sec=D))}[hands]
    lifted = {"forward": sym(lambda lat: limb(V(F, 1, U, .35, lat, .15), sec=D)), "head": a0, "sides": sym(lambda lat: limb(V(B, 1, U, .3, lat, .1), sec=D))}[hands]
    k0 = P("prone", aim=a0, legs={"ankle": 80})
    if alt:
        k1 = P("prone", spine=(-15, 0, 0), aim=arms_alt(lifted, a0), RL={"flex": -20, "ankle": 80}, LL={"ankle": 80})
        return [k0, k1, k0, mirror(k1)]
    k1 = P("prone", spine=(-25, 0, 0), neck=(-10, 0, 0), aim=lifted, legs={"flex": -15, "ankle": 80})
    return hold_or(c, [k0, k1])


def f_side_bend(c):
    b = chair() if c.seated else {}
    a = DOWN if re.search(r"cable|dumbbell|weight|kettlebell", c.name) else arms_alt(DOWN, OVERHEAD)
    k = K(b, {"spine": (0, 30, 0), "aim": a})
    return [K(b, {"aim": a if a is DOWN else arms_alt(DOWN, OVERHEAD)}), k] if not c.alt else [k, mirror(k)]


def f_windmill(c):
    trunk = {"pelvis": (V(LT, 1, D, .6, U, .1), F), "belly": (V(LT, 1, D, .7), F), "chest": (V(LT, 1, D, .8), F),
             "neck": (V(LT, 1, D, .6), F), "head": (V(LT, 1, U, .3), F)}
    legs_ = {"leg_upper.L": (V(D, 1, LT, .45), F), "leg_lower.L": (V(D, 1, LT, .45), F), "foot.L": (V(LT, 1, F, .3), U),
             "leg_upper.R": (V(D, 1, RT, .45), F), "leg_lower.R": (V(D, 1, RT, .45), F), "foot.R": (V(F, 1, LT, .3), U)}
    arms_ = {**one("L", limb(D, sec=F)), **one("R", limb(U, sec=F))}
    top = P(legs={"abd": 25}, aim=arms_alt(DOWN, OVERHEAD))
    return [top, P(aim={**trunk, **legs_, **arms_}, contacts=("feet.L", "feet.R", "y", 15))]


def f_turkish_get_up(c):
    up = one("L", limb(U, sec=F))
    k0 = P("supine", aim={**up, **one("R", {"arm_upper": (V(RT, 1, B, .3), U), "arm_lower": (V(RT, 1, B, .3), U), "hand": (RT, U)}),
                          "leg_upper.L": (V(B, 1, U, 1.2), U), "leg_lower.L": (V(D, .35, B), B), "foot.L": (B, U)})
    k1 = P("supine", pelvis=(55, 0, 0), aim={**up, **one("R", limb(V(D, 1, RT, .4), sec=B)),
                                              "leg_upper.L": (V(B, 1, U, 1.2), U), "leg_lower.L": (V(D, .35, B), B), "foot.L": (B, U)})
    k2 = K(half_kneel("L"), {"aim": {**up, **one("R", limb(V(D, 1, RT, .1), sec=F))}})
    k3 = P(aim={**up, **one("R", limb(V(D, 1, RT, .1), sec=F))})
    return [k0, k1, k2, k3]


def f_rollout(c, kneeling=True):
    if kneeling:
        k0 = K(kneel(), {"pelvis": (60, 0, 0), "aim": sym(lambda lat: limb(V(D, 1, F, .25), sec=F, hsec=V(-lat)))})
        k1 = P("prone", aim={**legs_both({"leg_lower": (B, D), "foot": (B, D)}), **sym(lambda lat: limb(V(F, 1, D, .2), sec=U, hsec=V(-lat)))},
               contacts=("hands", "knees"))
        return [k0, k1]
    return [plank_high(), P("prone", aim={**TOES, **sym(lambda lat: limb(V(F, 1, D, .3), sec=U))}, contacts=("hands", "feet"))]


def f_jackknife(c):
    return [plank_high(), down_dog()]


def f_lsit(c, hanging=False):
    if hanging:
        return [lifted(K({"legs": {"flex": 90}}, {"aim": HANG_UP}), .6)]
    return [P(legs={"flex": 90, "ankle": 20}, aim=sym(lambda lat: limb(V(D, 1, lat, .12), sec=B, hsec=F)))]


def f_inchworm(c):
    fold = P(pelvis=(115, 0, 0), spine=(15, 0, 0), aim=sym(lambda lat: limb(D, sec=F, hsec=V(-lat))), contacts=("hands", "feet"))
    return [P(aim=DOWN), fold, plank_high()]


def f_cat_cow(c):
    return list(USER["core/51"][1])


# --- Planks and push-ups ---
PUSH_LOW = sym(lambda lat: {"arm_upper": (V(B, .8, lat, .55, U, .25), D), "arm_lower": (D, F), "hand": (F, V(-lat))})


def push_arms(width=0.0):
    """Straight support arms, hands wider (width > 0) or narrower (< 0) than the shoulders."""
    return sym(lambda lat: limb(V(D, 1, lat, width), sec=F, hsec=V(-lat)))


def f_push_up(c, kind="std"):
    n = c.name
    width = 0.35 if c.wide else (-0.3 if c.close else 0.0)
    low = sym(lambda lat: {"arm_upper": (V(B, .7, lat, .55 + width, U, .25), D), "arm_lower": (V(D, 1, lat, width * .5), F), "hand": (F, V(-lat))})
    if c.close:
        low = sym(lambda lat: {"arm_upper": (V(B, 1, U, .25, lat, .1), D), "arm_lower": (V(D, 1, -lat, .25), F), "hand": (F, V(-lat))})
    legs = {**TOES}
    cont = ("hands", "feet")
    if c.kneel or kind == "kneel":
        legs = {"leg_lower": (B, D), "foot": (B, D)}
        cont = ("hands", "knees")
    if kind == "wall":
        orient = (V(U, 1, F, .5), V(F, 1, D, .5))
        return [P(orient, aim=sym(lambda lat: limb(F, sec=U))),
                P(orient, aim=sym(lambda lat: {"arm_upper": (V(lat, 1, B, .5), U), "arm_lower": (F, U), "hand": (F, U)}))]
    if kind == "pike":
        lo = down_dog(aim=sym(lambda lat: {"arm_upper": (V(D, 1, F, .2, lat, .4), F), "arm_lower": (V(D, 1, F, .1), F), "hand": (F, V(-lat))}))
        return [down_dog(), lo]
    if kind == "handstand":
        inv = {"leg_upper": (U, B), "leg_lower": (U, B), "foot": (U, B)}
        up = P("inverted", aim={**legs_both(inv), **sym(lambda lat: limb(V(D, 1, lat, .15), sec=B, hsec=V(-lat)))}, contacts=None)
        lo = P("inverted", aim={**legs_both(inv), **sym(lambda lat: {"arm_upper": (V(lat, 1, U, .3, F, .2), B), "arm_lower": (D, B), "hand": (F, V(-lat))})})
        return [up, lo]
    if kind == "hindu":
        swoop = P("prone", spine=(-10, 0, 0), aim={**legs_both(legs), **low}, contacts=cont)
        return [down_dog(), swoop, USER["pose/80"][1][0]]
    up = P("prone", aim={**legs_both(legs), **push_arms(width if not c.close else -0.3)}, contacts=cont)
    lo = P("prone", aim={**legs_both(legs), **low}, contacts=cont)
    keys = [up, lo]
    if kind == "t":
        keys.append(P("side_left", aim={"arm_upper.L": (D, RT), "arm_lower.L": (D, RT), "hand.L": (RT, F),
                                        "arm_upper.R": (U, RT), "arm_lower.R": (U, RT), "hand.R": (U, F), "foot": (RT, F)},
                      contacts=("hands.L", "feet")))
    if kind == "spider":
        sp = P("prone", aim={**legs_both(legs), **low, "foot.L": (B, D)}, LL={"flex": 90, "abd": 60, "knee": 100}, contacts=("hands", "feet.R"))
        keys = [up, sp, up, mirror(sp)]
    if kind == "clap":
        air = P("prone", aim={**legs_both(legs), **sym(lambda lat: limb(V(D, 1, lat, .1), sec=F))}, contacts=cont)
        keys = [lo, air]
    if kind == "row":
        keys += f_renegade_row(c)[1:2]
    if kind == "tap":
        tap = P("prone", aim={**legs_both(legs), **push_arms(), **one("L", {"arm_upper": (V(D, 1, RT, .5, U, .1), F), "arm_lower": (V(RT, 1, U, .4), F), "hand": (RT, D)})},
                contacts=[("hands", "feet")])
        keys = [up, tap, up, mirror(tap)]
    if kind == "one_arm":
        one_ = P("prone", legs={"abd": 15}, aim={**legs_both(legs), **one("L", limb(D, sec=F, hsec=RT)),
                                                 **one("R", {"arm_upper": (V(B, 1, U, .3), D), "arm_lower": (V(RT, 1, U, .5), D), "hand": (RT, U)})},
                 contacts=("hands", "feet"))
        keys = [one_]
    return hold_or(c, keys)


def f_mountain_climber(c, cross=False):
    drive = {"flex": 100, "knee": 110, **({"abd": -20} if cross else {})}
    k0 = plank_high()
    k1 = plank_high(LL=drive, aim={"foot.L": (B, D)})
    return [k1, mirror(k1)] if not c.hold else [k1]


def f_plank_jacks(c):
    return [plank_high(), plank_high(legs={"abd": 14})]


def f_bear(c, crawl=False):
    bear = {"leg_upper": (V(D, 1, F, .1), F), "leg_lower": (V(B, 1, D, .3), D), "foot": (D, F)}
    k = P("prone", aim={**SUPPORT_ARM, **legs_both(bear)}, contacts=("hands", "feet"))
    if crawl:
        step = P("prone", aim={**SUPPORT_ARM, **legs_both(bear), **one("L", limb(V(D, 1, F, .3), sec=F, hsec=RT)),
                               "leg_upper.R": (V(D, 1, F, .55), F)}, contacts=("hands", "feet"))
        return [step, mirror(step)]
    return [k]


def f_side_plank(c, kind="std"):
    base = dict(USER["plank/66"][1][0])
    if kind == "kneel":
        k = P("side_left", aim={"arm_upper.L": (D, RT), "arm_lower.L": (RT, F), "hand.L": (RT, F),
                                "arm_upper.R": (U, RT), "arm_lower.R": (U, RT), "hand.R": (U, F)},
              legs={"knee": 90, "flex": 10}, contacts=("forearms.L", "knees"))
        return [k]
    if kind == "straight":
        base = P("side_left", aim={"arm_upper.L": (D, RT), "arm_lower.L": (D, RT), "hand.L": (RT, F),
                                   "arm_upper.R": (U, RT), "arm_lower.R": (U, RT), "hand.R": (U, F), "foot": (RT, F)},
                 contacts=("hands.L", "feet"))
    if kind == "leg_lift":
        up = P("side_left", RL={"abd": 35}, aim={"arm_upper.L": (D, RT), "arm_lower.L": (RT, F), "hand.L": (RT, F),
                                                 "arm_upper.R": (U, RT), "arm_lower.R": (U, RT), "hand.R": (U, F), "foot.L": (RT, F)},
               contacts=("forearms.L", "feet.L"))
        return [base, up]
    if kind == "reach":
        under = P("side_left", aim={"arm_upper.L": (D, RT), "arm_lower.L": (RT, F), "hand.L": (RT, F),
                                    "arm_upper.R": (V(D, 1, RT, .5, LT, .0), U), "arm_lower.R": (V(D, 1, B, .3), U), "hand.R": (D, U),
                                    "foot": (RT, F)}, spine=(0, 0, -25), contacts=("forearms.L", "feet"))
        return [base, under]
    if kind == "tuck":
        tuck = P("side_left", RL={"flex": 90, "knee": 100}, aim={"arm_upper.L": (D, RT), "arm_lower.L": (RT, F), "hand.L": (RT, F),
                                                                 "arm_upper.R": (U, RT), "arm_lower.R": (U, RT), "hand.R": (U, F), "foot.L": (RT, F)},
                 contacts=("forearms.L", "feet.L"))
        return [base, tuck]
    if kind == "rotate":
        return [plank_high(), USER["move/34"][1][1] if False else base]
    if kind == "t":
        return [plank_high(), f_push_up(c, "t")[-1]]
    return [base]


def f_reverse_plank(c, leg=False):
    k = P("supine", aim={**sym(lambda lat: limb(V(D, 1, F, .05), sec=F, hsec=V(-lat))), **legs_both({"foot": (V(B, 1, U, .5), U)})},
          contacts=("hands", "feet"))
    if leg:
        return [k, P("supine", LL={"flex": 50}, aim={**sym(lambda lat: limb(V(D, 1, F, .05), sec=F, hsec=V(-lat))), "foot.R": (V(B, 1, U, .5), U)},
                     contacts=("hands", "feet.R"))]
    return [k]


def f_two_point(c):
    k = plank_high(aim={**reach_arm("L"), **reach_leg("R")}, contacts=("hands.R", "feet.L"))
    return [plank_high(), k, plank_high(), mirror(k)]


def f_spiderman_plank(c):
    k = plank_fore(LL={"flex": 90, "abd": 60, "knee": 100}, aim={"foot.L": (B, D)})
    return [k, mirror(k)]


def f_plank_to_stand(c):
    return [plank_high(), USER["pose/37"][1][0] if False else f_burpee(c)[1], P(aim=DOWN)]


def f_plank_hold(c, forearm=True):
    return [plank_fore()] if forearm else [plank_high()]


# --- Whole body, jumps and cardio ---
SQUAT_HANDS = sym(lambda lat: limb(V(D, 1, F, .55, lat, .1), sec=F, hsec=V(-lat)))


def f_burpee(c, extra=None):
    squat_down = K(squat_pose(1.1), {"aim": SQUAT_HANDS})
    jump = lifted(P(legs={"ankle": 40}, aim=OVERHEAD), .2)
    keys = [P(aim=DOWN), squat_down, plank_high(), squat_down, jump]
    if extra == "pullup":
        keys[-1] = lifted(K({"legs": {"flex": 10, "knee": 25}}, {"aim": PULLED}), .55)
    if extra == "high_pull":
        keys[-1] = P(legs={"ankle": 35}, aim=UPRIGHT_TOP)
    if extra == "box":
        keys[-1] = lifted(P(legs={"flex": 90, "knee": 100, "ankle": 30}, aim=OVERHEAD), .45)
    if extra == "pushup":
        keys = [P(aim=DOWN), squat_down, plank_high(), f_push_up(c)[1], squat_down, jump]
    if extra == "thrust":
        keys = [P(aim=DOWN), squat_down, plank_high(), squat_down]
    if extra == "seated":
        keys = [K(chair(), {"aim": DOWN}), K(chair(), {"pelvis": (60, 0, 0), "legs": lw(60, 90, 0), "aim": SQUAT_HANDS}), K(chair(), {"aim": OVERHEAD})]
    return keys


def f_man_maker(c):
    return [plank_high(), f_renegade_row(c)[1], K(squat_pose(1.0), {"aim": FRONT_RACK}), P(aim=OVERHEAD)]


def f_jumping_jack(c, kind="jack"):
    if kind == "split":
        k = K(split_stance("L", 0.3), {"aim": arms_alt(OVERHEAD, sym(lambda lat: limb(V(D, 1, B, .3), sec=F)))})
        return [k, mirror(k)]
    if kind == "squat":
        return [P(aim=HANDS_HIPS), K(squat_pose(0.8, wide=True), {"aim": HANDS_HIPS})]
    if kind == "seated":
        return [K(chair(), {"aim": DOWN}), K(chair(), {"legs": {**lw(0, 90, 0), "abd": 20}, "aim": OVERHEAD})]
    return [P(aim=DOWN), P(legs={"abd": 15, "ankle": 20}, aim=sym(lambda lat: limb(V(U, 1, lat, .45), sec=F)))]


def f_jump_rope(c, cross=False):
    rope = sym(lambda lat: {"arm_upper": (V(D, 1, lat, .2, F, .1), F), "arm_lower": (V(lat, 1, F, .5, D, .4), U), "hand": (V(lat, 1, F, .5), U)})
    if cross:
        rope = sym(lambda lat: {"arm_upper": (V(D, 1, F, .3, lat, .1), F), "arm_lower": (V(-lat, 1, F, .3, D, .3), U), "hand": (V(-lat), U)})
    return [P(legs={"ankle": 25}, aim=rope), P(legs={"ankle": 45, "knee": 10}, aim=rope)]


def stride(s=1.0, lean=5, arms=True, knee_up=0):
    """A running stride, left leg forward. s scales the stride (walk 0.5, run 1, sprint 1.3)."""
    front = lw(lean, 20 + 25 * s + knee_up, 5 * s, ankle=-5)
    back = lw(lean, -10 - 15 * s, -30 - 60 * s * s / 1.3, ankle=25 * s)
    a = {}
    if arms:
        fa = {"arm_upper": (V(D, 1, F, .5 * s, LT, .05), F), "arm_lower": (V(F, 1, U, .4 * s, RT, .2), B), "hand": (V(F, 1, U, .3), RT)}
        ba = {"arm_upper": (V(D, 1, B, .45 * s, RT, .05), F), "arm_lower": (V(D, .4, F, 1, LT, .1), B), "hand": (V(F, 1, D, .2), LT)}
        a = {**one("R", {k: (v[0], v[1]) for k, v in fa.items()}), **one("L", ba)}
        a = {**{f"{k}.R": (mirror_vec(v[0]) if False else v[0], v[1]) for k, v in fa.items()}, **{f"{k}.L": v for k, v in ba.items()}}
    return P(pelvis=(lean, 0, 0), LL=front, RL=back, aim=a)


def f_run(c, s=1.0, lean=5, knee_up=0):
    k = stride(s, lean, knee_up=knee_up)
    return [k, mirror(k)]


def f_high_knees(c, kind="knees"):
    if kind == "butt":
        k = P(RL=lw(0, -5, -130, ankle=40), LL={"ankle": 20}, aim=sym(lambda lat: {"arm_upper": (V(D, 1, lat, .1), F), "arm_lower": (V(F, 1, U, .3), B), "hand": (F, U)}))
    elif kind == "march":
        k = P(LL=lw(0, 70, 0), aim=arms_alt(sym(lambda lat: limb(V(D, 1, B, .3), sec=F)), sym(lambda lat: limb(V(D, 1, F, .5), sec=F))))
    else:
        k = P(LL=lw(0, 95, -5, ankle=20), RL={"ankle": 25}, aim=arms_alt(sym(lambda lat: {"arm_upper": (V(D, 1, B, .5), F), "arm_lower": (V(D, .3, F, 1), B), "hand": (F, U)}),
                                                                           sym(lambda lat: {"arm_upper": (V(D, 1, F, .6), F), "arm_lower": (V(U, 1, F, .4), B), "hand": (U, B)})))
    return [k, mirror(k)]


def f_bike(c):
    p = 45
    arms_ = sym(lambda lat: limb(V(F, 1, D, .7, lat, .05), sec=U, hsec=V(-lat)))
    k = P(pelvis=(p, 0, 0), spine=(10, 0, 0), neck=(-25, 0, 0), LL=lw(p, 75, -25), RL=lw(p, 30, 15, ankle=10), aim=arms_)
    return [k, mirror(k)]


def f_handcycle(c, back=False):
    a0 = sym(lambda lat: limb(V(F, 1, U, .3), sec=U))
    a1 = sym(lambda lat: limb(V(F, 1, D, .3), sec=U))
    k0, k1 = K(chair(), {"aim": arms_alt(a0, a1)}), K(chair(), {"aim": arms_alt(a1, a0)})
    return [k0, k1]


def f_rower(c):
    catch = P(pelvis=(20, 0, 0), legs={"flex": 155, "knee": 120, "ankle": -20}, aim=sym(lambda lat: limb(V(F, 1, D, .1), sec=U, hsec=V(-lat))))
    finish = P(pelvis=(-20, 0, 0), legs={"flex": 70, "ankle": 0},
               aim=sym(lambda lat: {"arm_upper": (V(B, 1, D, .7, lat, .2), F), "arm_lower": (V(F, 1, U, .1), U), "hand": (F, U)}))
    return [catch, finish]


def f_stepper(c, handles=True):
    a = sym(lambda lat: {"arm_upper": (V(D, 1, F, .3, lat, .1), F), "arm_lower": (V(F, 1, U, .2), U), "hand": (F, U)}) if handles else DOWN
    k = P(LL=lw(0, 55, 10), aim=a)
    return [k, mirror(k)]


def f_climb(c):
    return f_rope_climb(c)


def f_carry(c, arms="down", toes=False):
    a = {"down": HANG, "overhead": OVERHEAD, "waiter": arms_alt(sym(lambda lat: {"arm_upper": (V(U, 1, lat, .1), F), "arm_lower": (U, F), "hand": (V(U, 1, F, .3), D)}), HANG),
         "front": CHEST}[arms]
    k = P(LL=lw(0, 20, 5), RL=lw(0, -10, -25, ankle=20 if not toes else 45), aim=a)
    if toes:
        k = P(LL={**lw(0, 15, 5), "ankle": 35}, RL={**lw(0, -10, -25), "ankle": 45}, aim=a)
    return [k, mirror(k)] if not c.hold else [P(aim=a)]


def f_sled(c, kind="push"):
    lean = (V(U, 1, F, .9), V(F, 1, D, .9))
    if kind == "push":
        a = sym(lambda lat: {"arm_upper": (V(F, 1, D, .5, lat, .2), U), "arm_lower": (V(F, 1, U, .5), U), "hand": (F, D)})
        k = P(lean, LL={"flex": 70, "knee": 80, "ankle": -10}, RL={"flex": -5, "knee": 20, "ankle": 20}, aim=a)
        return [k, mirror(k)]
    if kind == "drag_back":
        a = sym(lambda lat: limb(V(F, 1, D, .5), sec=U))
        k = P(pelvis=(20, 0, 0), LL=lw(20, 60, -10), RL=lw(20, -10, -30), aim=a)
        return [k, mirror(k)]
    if kind == "drag":
        a = sym(lambda lat: limb(V(D, 1, B, .4), sec=F))
        k = P((V(U, 1, F, .4), V(F, 1, D, .4)), LL=lw(0, 45, 5), RL=lw(0, -20, -40, ankle=25), aim=a)
        return [k, mirror(k)]
    return f_row_seated(c, standing=True)


def f_tire_flip(c):
    return [K(squat_pose(1.0), {"pelvis": (55, 0, 0), "aim": sym(lambda lat: limb(V(D, 1, F, .4), sec=F, hsec=U))}),
            P(LL=lw(0, 30, 0), aim=sym(lambda lat: limb(V(F, 1, U, .6), sec=U)))]


def f_slam(c, kind="slam"):
    up = P(legs={"ankle": 20}, aim=sym(lambda lat: limb(V(U, 1, B, .25, lat, .1), sec=F)))
    if kind == "hammer":
        return [up, K(hinge(60, 30), {"aim": sym(lambda lat: limb(V(D, 1, F, .9), sec=F))})]
    if kind == "throw":
        return [K(half_squat(0.4), {"aim": sym(lambda lat: limb(V(U, 1, B, .5), sec=F))}), P(legs={"ankle": 20}, aim=sym(lambda lat: limb(V(F, 1, U, .9), sec=U)))]
    if kind == "side_throw":
        k = K(half_squat(0.4), {"spine": (0, 0, 45), "aim": sym(lambda lat: limb(V(F, 1, D, .3), sec=U))})
        return [k, mirror(k)]
    if kind == "chest_pass":
        return f_chest_press_standing(c)
    return [up, K(squat_pose(0.8), {"aim": sym(lambda lat: limb(V(D, 1, F, .8), sec=F))})]


def f_box_jump(c):
    return [K(squat_pose(0.7), {"aim": sym(lambda lat: limb(V(D, 1, B, .8), sec=F))}),
            lifted(P(legs={"flex": 80, "knee": 100, "ankle": 30}, aim=OVERHEAD), .45)]


def f_skater(c):
    k = P(pelvis=(30, 0, 0), LL=lw(30, 55, -20), RL={"flex": 10, "abd": -30, "knee": 40, "ankle": 30},
          aim=arms_alt(sym(lambda lat: limb(V(D, 1, B, .6), sec=F)), sym(lambda lat: limb(V(F, 1, LT, .5, D, .3), sec=U))))
    return [k, mirror(k)]


def f_knee_strike(c, cross=False):
    pull = sym(lambda lat: limb(V(D, 1, F, .9, -lat, .3 if cross else 0), sec=F))
    k = P(LL=lw(0, 100, 10, ankle=30), RL={"ankle": 25}, spine=(15, 0, -20 if cross else 0), aim=pull)
    return [P(aim=sym(lambda lat: limb(V(U, 1, F, .6, lat, .1), sec=F))), k]


def f_battle_rope(c, kind="alt"):
    b = half_squat(0.45)
    hi = sym(lambda lat: {"arm_upper": (V(F, 1, D, .3, lat, .1), U), "arm_lower": (V(F, 1, U, .5), U), "hand": (F, U)})
    lo = sym(lambda lat: {"arm_upper": (V(D, 1, F, .6, lat, .1), F), "arm_lower": (V(F, 1, D, .6), U), "hand": (F, D)})
    if kind == "double":
        return [K(b, {"aim": hi}), K(b, {"aim": lo})]
    if kind == "slam":
        return [P(legs={"ankle": 15}, aim=sym(lambda lat: limb(V(U, 1, F, .3), sec=F))), K(squat_pose(0.7), {"aim": sym(lambda lat: limb(V(D, 1, F, .9), sec=F))})]
    if kind == "side":
        k = K(b, {"spine": (0, 0, 25), "aim": sym(lambda lat: limb(V(F, 1, LT, .6, D, .3), sec=U))})
        return [k, mirror(k)]
    if kind == "circle":
        return [K(b, {"aim": hi}), K(b, {"aim": sym(lambda lat: limb(V(F, 1, lat, .7), sec=U))}), K(b, {"aim": lo})]
    if kind == "lunge":
        k = K(split_stance("L", 0.9), {"aim": arms_alt(hi, lo)})
        return [k, mirror(K(split_stance("L", 0.9), {"aim": arms_alt(lo, hi)}))]
    if kind == "kneel":
        return [K(kneel(), {"aim": arms_alt(hi, lo)}), K(b, {"aim": arms_alt(lo, hi)})]
    if kind == "side_plank":
        sp = dict(USER["plank/66"][1][0]); sp.update(one("R", limb(V(F, 1, RT, .5), sec=U)) if False else {})
        return [sp]
    if kind == "uppercut":
        k = K(b, {"spine": (0, 0, 20), "aim": arms_alt(sym(lambda lat: {"arm_upper": (V(F, 1, D, .3), U), "arm_lower": (V(U, 1, F, .3), B), "hand": (U, B)}), GUARD)})
        return [K(b, {"aim": GUARD}), k, K(b, {"aim": GUARD}), mirror(k)]
    if kind == "rainbow":
        k = K(b, {"aim": sym(lambda lat: limb(V(LT, 1, F, .5, D, .2), sec=U))})
        return [k, K(b, {"aim": sym(lambda lat: limb(V(U, 1, F, .5), sec=F))}), mirror(k)]
    return [K(b, {"aim": arms_alt(hi, lo)}), K(b, {"aim": arms_alt(lo, hi)})]


def f_chop(c, reverse=False, stability=False):
    b = base_for(c, half_squat(0.3))
    tw = 0 if stability else 40
    high = K(b, {"spine": (0, 0, tw), "aim": sym(lambda lat: limb(V(U, 1, LT, .8, F, .3), sec=F))})
    low = K(b, {"spine": (0, 0, -tw), "pelvis": (15, 0, 0), "aim": sym(lambda lat: limb(V(D, 1, RT, .7, F, .4), sec=F))})
    return [low, high] if reverse else [high, low]


def f_bob_weave(c):
    k = K(half_squat(0.5), {"spine": (15, 20, 0), "aim": GUARD})
    return [k, mirror(k)]


def f_ski_moguls(c):
    k = K(half_squat(0.45), {"pelvis": (14, 0, 25), "spine": (0, 0, -25), "aim": sym(lambda lat: limb(V(D, 1, F, .6, lat, .1), sec=F))})
    return [k, mirror(k)]


# --- Olympic lifts ---
def f_olympic(c, kind="clean"):
    n = c.name
    hang_start = "hang" in n
    wide = kind == "snatch"
    hold = sym(lambda lat: limb(V(D, 1, lat, .35 if wide else .08), sec=F))
    start = K(hinge(40, 25), {"aim": hold}) if hang_start else P(pelvis=(55, 0, 0), legs=lw(55, 65, -15), aim=hold)
    pull = P(legs={"ankle": 35}, aim=UPRIGHT_TOP)
    rack_sq = K(half_squat(0.5) if "power" in n else squat_pose(1.0), {"aim": FRONT_RACK})
    over_sq = K(half_squat(0.5) if "power" in n else squat_pose(1.0), {"aim": sym(lambda lat: limb(V(U, 1, lat, .35), sec=F))})
    if c.single_arm or "dumbbell_snatch" in n or "kettlebell_snatch" in n:
        over_sq = K(half_squat(0.4), {"aim": arms_alt(OVERHEAD, DOWN)})
        start = K(squat_pose(0.8), {"aim": arms_alt(sym(lambda lat: limb(V(D, 1, F, .4), sec=F)), DOWN)})
        return [start, P(legs={"ankle": 30}, aim=arms_alt(UPRIGHT_TOP, DOWN)), over_sq]
    if kind == "clean":
        return [start, pull, rack_sq]
    if kind == "snatch":
        return [start, pull, over_sq]
    if kind == "jerk":
        split = "split" in n
        dip = K(half_squat(0.35), {"aim": FRONT_RACK})
        top = K(split_stance("L", 0.45), {"aim": OVERHEAD}) if split else P(aim=OVERHEAD)
        return [dip, top]
    if kind == "clean_jerk":
        top = K(split_stance("L", 0.45), {"aim": OVERHEAD}) if "split" in n or "and_jerk" in n else P(aim=OVERHEAD)
        return [start, rack_sq, top]
    if kind == "high_pull":
        return [start, pull]
    if kind == "clean_press":
        return [start, rack_sq, P(aim=OVERHEAD)]
    return [start, pull, rack_sq]


# --- Wheelchair and seated upper-body moves ---
def f_seated_arms(c, kind="raise"):
    b = chair()
    d = {
        "raise": [DOWN, OVERHEAD],
        "forward": [DOWN, FORWARD],
        "prayer": [PRAYER],
        "prayer_up": [PRAYER_UP],
        "cactus": [CACTUS],
        "t": [DOWN, T_ARMS],
        "chest_expand": [FORWARD, sym(lambda lat: limb(V(lat, 1, B, .35), sec=F))],
        "low": [DOWN, sym(lambda lat: limb(V(F, 1, D, .6), sec=U))],
        "mid": [DOWN, FORWARD],
        "high": [DOWN, sym(lambda lat: limb(V(F, 1, U, 1.2), sec=U))],
        "reach": [DOWN, sym(lambda lat: limb(V(F, 1, D, .2), sec=U))],
        "ski": [sym(lambda lat: limb(V(F, 1, U, .5), sec=U)), sym(lambda lat: limb(V(D, 1, B, .5), sec=F))],
        "butterfly": [CACTUS, sym(lambda lat: {"arm_upper": (V(F, 1, lat, .2), U), "arm_lower": (U, B), "hand": (U, B)})],
        "rainbow": [sym(lambda lat: limb(V(LT, 1, U, .6), sec=F)), OVERHEAD, sym(lambda lat: limb(V(RT, 1, U, .6), sec=F))],
    }[kind]
    return [K(b, {"aim": a}) for a in d]


def f_seated_spine(c, kind="flex"):
    b = chair()
    if kind == "flex":  # seated cat cow
        return [K(b, {"spine": (30, 0, 0), "neck": (20, 0, 0), "aim": HANDS_HIPS if False else sym(lambda lat: limb(V(D, 1, F, .6), sec=F))}),
                K(b, {"spine": (-20, 0, 0), "neck": (-20, 0, 0), "aim": sym(lambda lat: limb(V(D, 1, F, .6), sec=F))})]
    if kind == "rotation":
        k = K(b, {"spine": (0, 0, 45), "aim": PRAYER})
        return [k, mirror(k)]
    if kind == "lateral":
        k = K(b, {"spine": (0, 30, 0), "aim": arms_alt(DOWN, OVERHEAD)})
        return [k, mirror(k)]
    if kind == "forward_fold":
        return [K(b, {"aim": DOWN}), K(b, {"pelvis": (45, 0, 0), "legs": lw(45, 90, 0), "spine": (35, 0, 0), "neck": (20, 0, 0),
                                           "aim": sym(lambda lat: limb(D, sec=F))})]
    if kind == "toe_touch":
        return [K(b, {"aim": OVERHEAD}), K(b, {"pelvis": (50, 0, 0), "legs": lw(50, 90, 0), "spine": (35, 0, 0), "aim": sym(lambda lat: limb(V(D, 1, F, .4), sec=F))})]
    if kind == "neck_flex":
        return [K(b, {"neck": (25, 0, 0), "head": (10, 0, 0)}), K(b, {"neck": (-25, 0, 0)})]
    if kind == "neck_lat":
        k = K(b, {"neck": (0, 30, 0)})
        return [k, mirror(k)]
    if kind == "neck_rot":
        k = K(b, {"neck": (0, 0, 50)})
        return [k, mirror(k)]
    if kind == "twist":
        k = K(b, {"spine": (0, 0, 40), "aim": sym(lambda lat: limb(V(F, 1, lat, .3), sec=U))})
        return [k, mirror(k)]
    if kind == "warrior":
        return [K(b, {"neck": (0, 0, 60), "aim": T_ARMS})]
    if kind == "reverse_warrior":
        return [K(b, {"spine": (0, -25, 0), "aim": arms_alt(OVERHEAD, DOWN)})]
    if kind == "triangle":
        return [K(b, {"spine": (0, 35, 0), "aim": arms_alt(DOWN, OVERHEAD)})]
    if kind == "hand_back":
        return [K(b, {"spine": (0, 0, 40), "aim": arms_alt(sym(lambda lat: limb(V(B, 1, D, .5, lat, .3), sec=F)), sym(lambda lat: limb(V(F, 1, D, .3, -lat, .5), sec=U)))})]
    if kind == "thread":
        return [K(b, {"spine": (20, 0, -40), "aim": arms_alt(sym(lambda lat: limb(V(RT, 1, D, .6), sec=F)), DOWN)})]
    return [K(b, {"aim": DOWN})]


# --- Yoga and stretches: named poses ---
WARRIOR2_LEGS = {"leg_upper.L": (V(LT, 1, D, .15), U), "leg_lower.L": (D, LT), "foot.L": (LT, U),
                 "leg_upper.R": (V(D, 1, RT, .7), F), "leg_lower.R": (V(D, 1, RT, .7), F), "foot.R": (V(F, 1, LT, .25), U)}
SIDE_LUNGE_LEGS = {"leg_upper.L": (V(LT, 1, F, .35, D, .3), U), "leg_lower.L": (V(D, 1, F, .1), LT), "foot.L": (V(F, 1, LT, .5), U),
                   "leg_upper.R": (V(RT, 1, D, .75), F), "leg_lower.R": (V(RT, 1, D, .75), F), "foot.R": (V(U, 1, RT, .3), RT)}
THUNDER_LEGS = legs_both({"leg_upper": (V(F, 1, D, .25), U), "leg_lower": (B, D), "foot": (B, D)})
HANDS_THIGHS = sym(lambda lat: limb(V(D, 1, F, .55, lat, .15), sec=F, hsec=V(-lat)))
TO_FLOOR = sym(lambda lat: limb(V(D, 1, lat, .05), sec=F, hsec=V(-lat)))
FLOOR_FWD = sym(lambda lat: limb(V(F, 1, D, .35), sec=U, hsec=V(-lat)))
PIGEON = {"leg_upper.L": (V(F, 1, LT, .35, D, .3), U), "leg_lower.L": (V(RT, 1, F, .25, D, .1), F), "foot.L": (V(RT, 1, B, .2), F),
          "leg_upper.R": (V(B, 1, D, .12), D), "leg_lower.R": (B, D), "foot.R": (B, D)}
TRICEPS_STRETCH = {**one("L", {"arm_upper": (V(U, 1, LT, .1), RT), "arm_lower": (V(D, 1, B, .3, RT, .35), F), "hand": (V(D, 1, B, .2), F)}),
                   **one("R", {"arm_upper": (V(U, 1, F, .35, LT, .2), F), "arm_lower": (V(LT, 1, U, .5, B, .2), F), "hand": (LT, F)})}
COW_FACE_ARMS = {**one("L", {"arm_upper": (V(U, 1, LT, .1), RT), "arm_lower": (V(D, 1, B, .4, RT, .25), F), "hand": (D, F)}),
                 **one("R", {"arm_upper": (V(D, 1, B, .25), F), "arm_lower": (V(U, 1, B, .35, LT, .25), B), "hand": (U, B)})}


def warrior2(**kw):
    return P(neck=(0, 0, 70), aim={**WARRIOR2_LEGS, **T_ARMS, **kw.pop("aim", {})}, contacts=("feet.L", "feet.R", "y", 15), **kw)


def warrior1(**kw):
    return P(LL=lw(0, 80, -5), RL={"flex": -52, "knee": 0, "ankle": -25}, aim={**PRAYER_UP, **kw.pop("aim", {})},
             contacts=("feet.L", "feet.R", "x", 15), **kw)


def fold(p=110, **kw):
    return P(pelvis=(p, 0, 0), spine=(20, 0, 0), neck=(20, 0, 0), legs=lw(p, 0, 0), aim={**TO_FLOOR, **kw.pop("aim", {})}, **kw)


def seated_fold(**kw):
    return seated(pelvis=(25, 0, 0), spine=(40, 0, 0), neck=(15, 0, 0), **kw)


def yoga(name):
    """Keys for a named yoga pose (Garmin's pose category, also used for stretches)."""
    Y = {
        "mountain": lambda: [P(aim=sym(lambda lat: limb(V(D, 1, lat, .15), sec=F)))],
        "standing_arms_up": lambda: [P(aim=PRAYER_UP)],
        "tree": lambda: [P(RL={"flex": 45, "abd": 50, "rot": 60, "knee": 140, "ankle": 30}, aim=PRAYER_UP)],
        "chair": lambda: [P(pelvis=(30, 0, 0), legs=lw(30, 60, -25), aim=sym(lambda lat: limb(V(U, 1, F, .55, lat, .1), sec=F)))],
        "eagle": lambda: [P(pelvis=(20, 0, 0), LL=lw(20, 40, -20), RL={"flex": 70, "abd": -30, "knee": 70, "ankle": 30},
                            aim=sym(lambda lat: {"arm_upper": (V(F, 1, U, .4, -lat, .3), U), "arm_lower": (V(U, 1, F, .1), B), "hand": (U, B)}))],
        "warrior_one": lambda: [warrior1()],
        "warrior_two": lambda: [warrior2()],
        "warrior_three": lambda: [P(pelvis=(90, 0, 0), LL=lw(90, 0, 0), RL={"flex": 0, "ankle": 10}, arms={"flex": 180})],
        "peaceful_warrior": lambda: [P(spine=(0, -25, 0), neck=(-10, 0, 0), aim={**WARRIOR2_LEGS, **one("L", limb(V(U, 1, RT, .45), sec=F)),
                                                                                  **one("R", limb(V(D, 1, RT, .35, B, .2), sec=F))},
                                       contacts=("feet.L", "feet.R", "y", 15))],
        "devotional_warrior": lambda: [P(pelvis=(80, 0, 0), spine=(20, 0, 0), LL=lw(80, 80, -5), RL={"flex": 80 - 52, "knee": 0, "ankle": -25},
                                         aim=sym(lambda lat: limb(V(U, 1, B, .6, -lat, .1), sec=B)), contacts=("feet.L", "feet.R", "x", 15))],
        "triangle": lambda: [f_windmill(None)[1]],
        "revolved_triangle": lambda: [P(pelvis=(85, 0, 0), spine=(0, 0, -60), LL=lw(85, 25, 25), RL=lw(85, -25, -25),
                                        aim={**one("R", limb(D, sec=F)), **one("L", limb(U, sec=F))}, contacts=("feet.L", "feet.R", "x", 15))],
        "half_moon": lambda: [P(aim={"pelvis": (V(LT, 1, D, .1), F), "belly": (V(LT, 1, D, .05), F), "chest": (LT, F), "neck": (LT, F),
                                     "head": (V(LT, 1, U, .1), F), "leg_upper.L": (D, F), "leg_lower.L": (D, F), "foot.L": (F, U),
                                     "leg_upper.R": (V(RT, 1, U, .08), F), "leg_lower.R": (V(RT, 1, U, .08), F), "foot.R": (V(RT, 1, F, .2), F),
                                     **one("L", limb(D, sec=F)), **one("R", limb(U, sec=F))})],
        "revolved_half_moon": lambda: [P(pelvis=(90, 0, 0), spine=(0, 0, -60), LL=lw(90, 0, 0), RL={"flex": 0, "ankle": 10},
                                         aim={**one("R", limb(D, sec=F)), **one("L", limb(U, sec=F))})],
        "bowed_half_moon": lambda: [P(spine=(0, 30, 0), aim=PRAYER_UP)],
        "standing_side_opener": lambda: [P(spine=(0, 30, 0), aim=arms_alt(DOWN, OVERHEAD))],
        "standing_forward_bend": lambda: [fold()],
        "standing_forward_bend_halfway_up": lambda: [P(pelvis=(85, 0, 0), spine=(-5, 0, 0), legs=lw(85, 0, 0),
                                                       aim=sym(lambda lat: limb(V(D, 1, F, .35), sec=F, hsec=V(-lat))))],
        "standing_single_leg_forward_bend": lambda: [P(pelvis=(100, 0, 0), spine=(20, 0, 0), LL=lw(100, 22, 22), RL=lw(100, -22, -22),
                                                       aim=TO_FLOOR, contacts=("feet.L", "feet.R", "x", 15))],
        "standing_split": lambda: [P(pelvis=(120, 0, 0), spine=(20, 0, 0), LL=lw(120, 0, 0), RL={"flex": -30, "knee": 0, "ankle": 30},
                                     aim=TO_FLOOR, contacts=("hands", "feet.L"))],
        "standing_wide_leg_forward_bend": lambda: [P(pelvis=(110, 0, 0), spine=(20, 0, 0), neck=(20, 0, 0), legs={"flex": 110, "abd": 30, "ankle": 0},
                                                     aim=TO_FLOOR, contacts=("hands", "feet"))],
        "half_chair_half_ankle_to_knee": lambda: [P(pelvis=(30, 0, 0), LL=lw(30, 55, -25), RL={"flex": 95, "abd": 30, "rot": 80, "knee": 90}, aim=PRAYER)],
        "high_lunge": lambda: [USER["lunge/34"][1][1]],
        "low_lunge": lambda: [low_lunge("L", aim=PRAYER_UP)],
        "low_lunge_with_knee_down": lambda: [low_lunge("L", aim=PRAYER_UP)],
        "runners_lunge": lambda: [USER["pose/37"][1][0]],
        "wide_side_lunge": lambda: [P(pelvis=(30, 0, 0), aim={**SIDE_LUNGE_LEGS, **PRAYER}, contacts=("feet.L", "feet.R", "y", 15))],
        "gate": lambda: [P(spine=(0, 30, 0), aim={"leg_upper.R": (D, F), "leg_lower.R": (B, D), "foot.R": (B, D),
                                                    "leg_upper.L": (V(LT, 1, D, .6), F), "leg_lower.L": (V(LT, 1, D, .6), F), "foot.L": (V(LT, 1, F, .3), U),
                                                    **one("L", limb(V(D, 1, LT, .5), sec=F)), **one("R", limb(V(U, 1, LT, .8), sec=F))},
                           contacts=("feet.L", "knees.R", "y"))],
        "mermaid": lambda: [seated(spine=(0, 30, 0), aim={**USER["warm_up/74"][1][0], **arms_alt(sym(lambda lat: limb(V(D, 1, lat, .3), sec=F)), OVERHEAD)})
                            if False else P(spine=(0, 30, 0), legs={"flex": 90}, aim={**{k: v for k, v in USER["warm_up/74"][1][0].items() if k.startswith(("leg", "foot"))},
                                                                                      **one("L", limb(V(D, 1, LT, .3), sec=F)), **one("R", limb(V(U, 1, LT, .7), sec=F))})],
        "bound_angle": lambda: [P(legs={"flex": 85, "abd": 50, "rot": 50, "knee": 130, "ankle": 10}, aim=sym(lambda lat: limb(V(D, 1, F, .6), sec=F)))],
        "easy_seated": lambda: [seated(aim=sym(lambda lat: limb(V(D, 1, F, .35, lat, .5), sec=F)), **CROSS_LEGS)],
        "prayer_mudra": lambda: [seated(aim=PRAYER, **CROSS_LEGS)],
        "thunderbolt": lambda: [P(aim={**THUNDER_LEGS, **HANDS_THIGHS})],
        "heros": lambda: [P(aim={**THUNDER_LEGS, **HANDS_THIGHS})],
        "thunderbolt_pose_both_sides_arm_stretch": lambda: [P(spine=(0, 25, 0), aim={**THUNDER_LEGS, **arms_alt(sym(lambda lat: limb(V(D, 1, lat, .4), sec=F)), OVERHEAD)})],
        "staff": lambda: [seated(legs={"ankle": -20}, aim=sym(lambda lat: limb(V(D, 1, B, .4, lat, .12), sec=F)))],
        "seated_wide_leg_forward_bend": lambda: [seated(legs={"abd": 40, "ankle": -15}, pelvis=(30, 0, 0), spine=(35, 0, 0), aim=FLOOR_FWD)],
        "head_to_knee": lambda: [seated_fold(RL={"flex": 90, "abd": 60, "rot": 70, "knee": 140}, aim=FLOOR_FWD)],
        "bound_seated_single_leg_forward_bend": lambda: [seated_fold(RL={"flex": 90, "abd": 60, "rot": 70, "knee": 140}, aim=FLOOR_FWD)],
        "revolved_head_to_knee": lambda: [seated(LL={"abd": 45}, RL={"flex": 90, "abd": 60, "rot": 70, "knee": 140}, spine=(0, 35, 0),
                                                 aim=arms_alt(sym(lambda lat: limb(V(D, 1, lat, .5), sec=F)), OVERHEAD))],
        "cow_face": lambda: [seated(legs={"flex": 85, "abd": -15, "rot": 45, "knee": 150}, aim=COW_FACE_ARMS)],
        "ankle_to_knee": lambda: [seated(aim=HANDS_THIGHS, **CROSS_LEGS)],
        "heron": lambda: [P(aim={**{k: v for k, v in THUNDER_LEGS.items() if k.endswith(".L")}, **sym(lambda lat: limb(V(F, 1, U, 1.3), sec=U))},
                            RL={"flex": 150, "knee": 0, "ankle": 0})],
        "boat": lambda: f_vsit(None, "forward"),
        "side_boat": lambda: f_side_plank(C("plank", "side_plank"), "straight"),
        "bow": lambda: [P("prone", spine=(-30, 0, 0), neck=(-15, 0, 0), legs={"flex": -25, "knee": 110, "ankle": 30},
                          aim=sym(lambda lat: limb(V(B, 1, U, .55, lat, .1), sec=D)))],
        "locust": lambda: [P("prone", spine=(-25, 0, 0), neck=(-10, 0, 0), legs={"flex": -15, "ankle": 80},
                             aim=sym(lambda lat: limb(V(B, 1, U, .35, lat, .1), sec=D)))],
        "sphinx": lambda: [P("prone", spine=(-35, 0, 0), legs={"ankle": 85}, aim=FOREARM, contacts=("forearms", "feet"))],
        "baby_cobra": lambda: [P("prone", spine=(-25, 0, 0), neck=(-5, 0, 0), legs={"ankle": 85},
                                 aim=sym(lambda lat: {"arm_upper": (V(B, 1, U, .8, lat, .25), D), "arm_lower": (V(D, 1, F, .2), F), "hand": (F, V(-lat))}))],
        "fish": lambda: [P("supine", spine=(-35, 0, 0), neck=(-40, 0, 0), legs={"ankle": 30}, aim=sym(lambda lat: limb(V(B, 1, D, .1), sec=U)))],
        "plow": lambda: [P("supine", pelvis=(-80, 0, 0), spine=(40, 0, 0),
                           aim={**legs_both({"leg_upper": (V(F, 1, D, .7), D), "leg_lower": (V(F, 1, D, .7), D), "foot": (V(D, 1, F, .3), F)}), **SUP_ARMS_FLOOR},
                           contacts=("feet", "back"))],
        "shoulder_stand": lambda: [P("supine", pelvis=(-88, 0, 0), spine=(25, 0, 0),
                                     aim={**legs_both({"leg_upper": (U, F), "leg_lower": (U, F), "foot": (U, F)}),
                                          **sym(lambda lat: {"arm_upper": (V(B, 1, U, .25, lat, .2), U), "arm_lower": (V(U, 1, B, .1), F), "hand": (U, F)})})],
        "supine_spinal_twist": lambda: [P("supine", pelvis=(0, 0, 70), spine=(0, 0, -70), legs={"flex": 90, "knee": 90},
                                          aim=sym(lambda lat: limb(lat, sec=U)))],
        "reclined_hand_to_foot": lambda: [P("supine", LL={"flex": 90, "ankle": 0}, aim={**one("L", limb(V(U, 1, B, .25), sec=B)), **one("R", limb(V(B, 1, RT, .3), sec=U))})],
        "knees_chest_chin": lambda: [P("prone", pelvis=(35, 0, 0), aim={**KNEEL, **sym(lambda lat: {"arm_upper": (V(B, 1, U, 1.2, lat, .2), D), "arm_lower": (D, F), "hand": (F, V(-lat))})},
                                       contacts=("knees", "back"))],
        "four_limbed_staff": lambda: [f_push_up(C("push_up", "push_up"))[1]],
        "upward_facing_plank": lambda: f_reverse_plank(None),
        "plank": lambda: [plank_high()],
        "dolphin_plank": lambda: [plank_fore()],
        "side_plank": lambda: [USER["plank/66"][1][0]],
        "down_dog_knee_to_nose": lambda: [down_dog(aim={"leg_upper.R": (V(F, 1, D, .5), U), "leg_lower.R": (V(B, 1, D, .5), D), "foot.R": (B, D)})],
        "down_dog_split": lambda: [down_dog(aim={"leg_upper.R": (V(B, 1, U, .6), D), "leg_lower.R": (V(B, 1, U, .6), D), "foot.R": (V(B, 1, U, .6), D)})],
        "down_dog_split_open_hip_bent_knee": lambda: [down_dog(aim={"leg_upper.R": (V(B, 1, U, .8, RT, .3), D), "leg_lower.R": (V(LT, 1, U, .6), B), "foot.R": (V(LT, 1, U, .3), B)})],
        "one_legged_downward_facing_pose_open_hip_bent_knee": lambda: [down_dog(aim={"leg_upper.R": (V(B, 1, U, .8, RT, .3), D), "leg_lower.R": (V(LT, 1, U, .6), B), "foot.R": (V(LT, 1, U, .3), B)})],
        "one_legged_pigeon": lambda: [P(spine=(-5, 0, 0), aim={**PIGEON, **TO_FLOOR})],
        "full_split": lambda: [P(aim={"leg_upper.L": (V(F, 1, D, .05), U), "leg_lower.L": (V(F, 1, D, .05), U), "foot.L": (V(F, 1, U, .3), U),
                                      "leg_upper.R": (B, D), "leg_lower.R": (B, D), "foot.R": (B, D), **PRAYER_UP})],
        "wheel": lambda: [P("supine", pelvis=(-55, 0, 0), spine=(-50, 0, 0), neck=(-30, 0, 0),
                            aim={**sym(lambda lat: {"arm_upper": (V(D, 1, F, .25, lat, .1), F), "arm_lower": (V(D, 1, F, .1), F), "hand": (B, V(-lat))}),
                                 **legs_both({"leg_upper": (V(B, 1, D, .6), U), "leg_lower": (V(D, 1, F, .1), B), "foot": (B, U)})},
                            contacts=("hands", "feet"))],
        "squat_palm_press": lambda: [K(squat_pose(1.15, wide=True), {"spine": (-25, 0, 0), "aim": PRAYER})],
        "cat": lambda: [USER["core/51"][1][0]],
        "cow": lambda: [USER["core/51"][1][1]],
        "all_fours": lambda: [quad()],
        "table_top": lambda: [quad()],
        "thread_the_needle": lambda: [quad(spine=(0, 0, -40), aim={"arm_upper.R": (V(LT, 1, D, .6), D), "arm_lower.R": (LT, D), "hand.R": (LT, D)})],
        "corpse": lambda: [P("supine", legs={"abd": 8, "ankle": 25}, aim=sym(lambda lat: limb(V(B, 1, lat, .35), sec=U)))],
        "extended_puppy": lambda: list(USER["pose/24"][1]),
        "childs": lambda: list(USER["pose/11"][1]),
    }
    return Y.get(name)


def stretch(name):
    """Keys for common stretches and warm-ups (warm_up, move and banded categories)."""
    S = {
        "quad": lambda: [P(RL=lw(0, -5, -165, ankle=40), aim=arms_alt(sym(lambda lat: limb(V(D, 1, lat, .25), sec=F)),
                                                                      sym(lambda lat: limb(V(D, 1, B, .55), sec=F))))],
        "triceps": lambda: [P(aim=TRICEPS_STRETCH)],
        "calf": lambda: [P((V(U, 1, F, .25), V(F, 1, D, .25)), LL=lw(0, 40, 15), RL={"flex": -12, "knee": 0, "ankle": -18},
                           aim=sym(lambda lat: limb(V(F, 1, U, .35), sec=U)), contacts=("feet.L", "feet.R", "x", 15))],
        "standing_hamstring": lambda: [P(pelvis=(55, 0, 0), LL={"flex": 80, "knee": 0, "ankle": -30}, RL=lw(55, -10, -35),
                                         aim=HANDS_THIGHS, contacts=("feet.L", "feet.R", "x", 15))],
        "hip_flexor_quad": lambda: [low_lunge("L", RL={"knee": 150, "ankle": 30}, aim=arms_alt(sym(lambda lat: limb(V(D, 1, F, .5), sec=F)), sym(lambda lat: limb(V(D, 1, B, .7), sec=F))))],
        "pectoral": lambda: [P(aim=sym(lambda lat: limb(V(lat, 1, B, .45), sec=F)))],
        "biceps": lambda: [P(aim=sym(lambda lat: limb(V(D, 1, B, .8, lat, .35), sec=F)))],
        "over_under": lambda: [P(aim=COW_FACE_ARMS)],
        "levator": lambda: [P(neck=(25, 0, 35), head=(10, 0, 0))],
        "neck": lambda: [P(neck=(0, 35, 0)), mirror(P(neck=(0, 35, 0)))],
        "forearms": lambda: [P(aim={**one("L", {"arm_upper": (F, U), "arm_lower": (F, U), "hand": (V(D, 1, F, .3), U)}),
                                    **one("R", {"arm_upper": (V(F, 1, D, .4, LT, .3), U), "arm_lower": (V(F, 1, LT, .5), U), "hand": (V(D, 1, F, .3), LT)})})],
        "side": lambda: [P(spine=(0, 25, 0), aim=PRAYER_UP), mirror(P(spine=(0, 25, 0), aim=PRAYER_UP))],
        "side_one": lambda: [P(spine=(0, 25, 0), aim=arms_alt(sym(lambda lat: limb(V(D, 1, lat, .2), sec=F)), OVERHEAD))],
        "standing_it_band": lambda: [P(LL={"abd": -10}, RL={"abd": 14, "flex": -5}, spine=(0, 25, 0), aim=PRAYER_UP)],
        "lying_knee_to_chest": lambda: [P("supine", LL={"flex": 125, "knee": 125, "ankle": 20},
                                          aim={**one("L", limb(V(B, 1, U, .5), sec=U)), **one("R", limb(V(B, 1, RT, .2), sec=U))})],
        "lying_it_band": lambda: [P("supine", RL={"flex": 80, "abd": -50}, aim=sym(lambda lat: limb(lat, sec=U)))],
        "lying_abduction": lambda: [P("supine", legs={"flex": 90, "abd": 40, "ankle": 20}, aim=SUP_ARMS_FLOOR)],
        "wall_chest": lambda: [P(spine=(0, 0, -30), aim=arms_alt(sym(lambda lat: limb(V(lat, 1, B, .45), sec=F)), DOWN))],
        "sleeper": lambda: [P("side_left", aim={"arm_upper.L": (V(RT, 1, F, .1), F), "arm_lower.L": (V(U, 1, RT, .2), RT), "hand.L": (U, RT),
                                                "arm_upper.R": (V(RT, 1, U, .5), F), "arm_lower.R": (V(D, 1, RT, .5), F), "hand.R": (D, RT)},
                              legs={"flex": 30, "knee": 40})],
        "scorpion": lambda: [P("prone", pelvis=(0, 0, -35), RL={"flex": -20, "abd": -70, "knee": 80}, aim=sym(lambda lat: limb(lat, sec=D)))],
        "groiner": lambda: [P("prone", aim={**SUPPORT_ARM, **TOES, "leg_upper.L": (V(F, 1, D, .3, LT, .35), D), "leg_lower.L": (V(D, 1, B, .2), F),
                                            "foot.L": (F, U)}, contacts=("hands", "feet"))],
        "worlds_greatest": lambda: [K(split_stance("L", 0.85), {"pelvis": (55, 0, 0), "spine": (0, 0, 35), "aim": {**one("R", limb(D, sec=F)), **one("L", limb(U, sec=F))}})],
        "leg_swing": lambda: [P(RL={"flex": 60, "ankle": 10}, aim=HANDS_HIPS), P(RL={"flex": -30, "ankle": 20}, aim=HANDS_HIPS)],
        "leg_swing_side": lambda: [P(RL={"flex": 10, "abd": -30}, aim=HANDS_HIPS), P(RL={"abd": 40}, aim=HANDS_HIPS)],
        "high_kick": lambda: [P(RL={"flex": 85, "ankle": 0}, aim=arms_alt(sym(lambda lat: limb(V(F, 1, U, .3, RT, .5), sec=U)), sym(lambda lat: limb(V(D, 1, B, .5), sec=F))))],
        "knee_hug": lambda: [P(LL={"ankle": 25}, RL={"flex": 120, "knee": 130, "ankle": 20},
                               aim=sym(lambda lat: {"arm_upper": (V(D, 1, F, .6, lat, .2), F), "arm_lower": (V(F, 1, -lat, .6, U, .2), U), "hand": (V(-lat, 1, F, .3), U)}))],
        "leg_cradle": lambda: [P(RL={"flex": 90, "abd": 30, "rot": 70, "knee": 110}, aim=sym(lambda lat: {"arm_upper": (V(D, 1, F, .6), F), "arm_lower": (V(F, 1, RT, .5), U), "hand": (V(RT, 1, F, .3), U)}))],
        "inverted_hamstring": lambda: [P(pelvis=(85, 0, 0), LL=lw(85, 12, 0), RL={"flex": 5, "ankle": 30}, aim=T_ARMS)],
        "reach_roll_lift": lambda: [USER["pose/11"][1][0], P(aim={**{k: v for k, v in USER["pose/11"][1][0].items() if not k.startswith(("arm_upper.L", "arm_lower.L", "hand.L"))},
                                                                 **one("L", limb(V(F, 1, U, .4), sec=U))})],
        "thoracic_rotation": lambda: [quad(spine=(0, 0, 40), aim={"arm_upper.L": (V(LT, 1, U, .7), D), "arm_lower.L": (V(RT, 1, U, .4, F, .3), D), "hand.L": (RT, D)},
                                           ) if False else quad(spine=(0, 0, 40), aim={"arm_upper.L": (V(LT, 1, U, .7), D), "arm_lower.L": (V(RT, 1, U, .4, F, .3), D), "hand.L": (RT, D)})],
        "windshield": lambda: [P("supine", pelvis=(0, 0, 40), spine=(0, 0, -40), legs={"flex": 90, "ankle": 20}, aim=sym(lambda lat: limb(lat, sec=U))),
                               mirror(P("supine", pelvis=(0, 0, 40), spine=(0, 0, -40), legs={"flex": 90, "ankle": 20}, aim=sym(lambda lat: limb(lat, sec=U))))],
        "frog": lambda: [P("prone", aim={**FOREARM, **sym(lambda lat: {"leg_upper": (V(lat, 1, D, .7), F), "leg_lower": (B, D), "foot": (B, D)})}, contacts=("forearms", "knees"))],
        "foam_back": lambda: [P("supine", pelvis=(-20, 0, 0), spine=(-15, 0, 0), aim={**legs_both(HOOK), **behind_head_aim()}, contacts=("feet", "back"))],
        "foam_lat": lambda: [P("side_left", aim={"arm_upper.L": (F, U), "arm_lower.L": (F, U), "hand.L": (F, RT), "arm_upper.R": (V(RT, 1, D, .3), F),
                                                 "arm_lower.R": (D, F), "hand.R": (D, F)}, legs={"flex": 20, "knee": 30})],
        "reverse_t": lambda: [P(pelvis=(90, 0, 0), LL=lw(90, 0, 0), RL={"flex": 0, "ankle": 10}, aim=T_ARMS),
                              P(pelvis=(90, 0, 0), spine=(0, 0, 60), LL=lw(90, 0, 0), RL={"flex": 0, "ankle": 10, "rot": 40}, aim=T_ARMS)],
        "half_kneel_rotation": lambda: [K(half_kneel("L"), {"aim": FORWARD}), K(half_kneel("L"), {"spine": (0, 0, 50), "aim": arms_alt(FORWARD, sym(lambda lat: limb(V(B, 1, lat, .3), sec=F)))})],
        "ankle": lambda: [P(RL={"flex": 35, "ankle": 35}, aim=HANDS_HIPS), P(RL={"flex": 35, "ankle": -25}, aim=HANDS_HIPS)],
        "ankle_seated": lambda: [seated(legs={"ankle": 30}, aim=sym(lambda lat: limb(V(D, 1, lat, .12, B, .1), sec=F))),
                                 seated(legs={"ankle": -25}, aim=sym(lambda lat: limb(V(D, 1, lat, .12, B, .1), sec=F)))],
        "neck_tilt": lambda: [P(neck=(0, 30, 0)), mirror(P(neck=(0, 30, 0)))],
        "quad_rock": lambda: [quad(), P(aim={**{k: v for k, v in USER["pose/11"][1][0].items()}})],
        "hip_circles_standing": lambda: [P(pelvis=(0, 12, 0), spine=(0, -12, 0), aim=HANDS_HIPS), mirror(P(pelvis=(0, 12, 0), spine=(0, -12, 0), aim=HANDS_HIPS))],
        "upper_back": lambda: [P(spine=(25, 0, 0), neck=(15, 0, 0), aim=sym(lambda lat: limb(V(F, 1, -lat, .25), sec=U)))],
        "shoulder_cross": lambda: list(USER["move/2"][1]),
        "hamstring_supine": lambda: [P("supine", LL={"flex": 90, "ankle": 0}, aim={**one("L", limb(V(U, 1, B, .25), sec=B)), **one("R", limb(V(U, 1, B, .25), sec=B))})],
        "butterfly": lambda: yoga("bound_angle")(),
        "straddle": lambda: yoga("seated_wide_leg_forward_bend")(),
        "pigeon": lambda: yoga("one_legged_pigeon")(),
        "front_split": lambda: yoga("full_split")(),
        "lat": lambda: [P(spine=(0, 25, 0), aim=PRAYER_UP)],
        "shoulder_rolls": lambda: [P(aim=DOWN), P(aim=DOWN, neck=(-5, 0, 0))],
        "tick_tock": lambda: [P("supine", pelvis=(0, 0, 30), spine=(0, 0, -30), legs={"flex": 90, "ankle": 20}, aim=sym(lambda lat: limb(lat, sec=U))),
                              mirror(P("supine", pelvis=(0, 0, 30), spine=(0, 0, -30), legs={"flex": 90, "ankle": 20}, aim=sym(lambda lat: limb(lat, sec=U))))],
        "twist_standing": lambda: [P(spine=(0, 0, 45), aim=sym(lambda lat: limb(V(F, 1, lat, .4, D, .3), sec=U))), mirror(P(spine=(0, 0, 45), aim=sym(lambda lat: limb(V(F, 1, lat, .4, D, .3), sec=U))))],
        "seated_twist": lambda: list(USER["pose/55"][1]),
        "saw": lambda: [seated(legs={"abd": 35, "ankle": -15}, pelvis=(20, 0, 0), spine=(35, 0, -45), aim=arms_alt(sym(lambda lat: limb(V(B, 1, lat, .5, U, .2), sec=F)), sym(lambda lat: limb(V(F, 1, LT, .6, D, .4), sec=U))))],
        "toe_touch": lambda: [P(aim=OVERHEAD), fold()],
        "forward_fold": lambda: [fold()],
        "hang": lambda: [fold(p=115, aim=sym(lambda lat: limb(D, sec=F)))],
        "shaving_head": lambda: [P(aim=BEHIND_HEAD), P(aim=sym(lambda lat: limb(V(U, 1, F, .3, lat, .35), sec=F)))],
        "hug_tree": lambda: [P(aim=T_ARMS), P(aim=sym(lambda lat: {"arm_upper": (V(F, 1, lat, .7), U), "arm_lower": (V(F, 1, -lat, .7), U), "hand": (V(-lat), U)}))],
        "scap": lambda: [P(aim=FORWARD), P(aim=sym(lambda lat: limb(V(F, 1, lat, .2), sec=U)))],
        "breathing_supine": lambda: [P("supine", aim={**legs_both(HOOK), **sym(lambda lat: limb(V(B, 1, U, .2, -lat, .2), sec=U))})],
        "breathing_seated": lambda: [seated(aim=sym(lambda lat: limb(V(D, 1, F, .35, lat, .5), sec=F)), **CROSS_LEGS)],
        "throat_lock": lambda: [seated(neck=(30, 0, 0), aim=HANDS_THIGHS, **CROSS_LEGS)],
        "arch_curl": lambda: [seated(spine=(-15, 0, 0), aim=HANDS_THIGHS, legs={"flex": 100, "knee": 90}), seated(spine=(35, 0, 0), neck=(20, 0, 0), aim=HANDS_THIGHS, legs={"flex": 100, "knee": 90})],
        "half_roll_up": lambda: [seated(legs={"flex": 100, "knee": 90}, aim=FORWARD), seated(pelvis=(-30, 0, 0), spine=(30, 0, 0), legs={"flex": 70, "knee": 90}, aim=FORWARD)],
        "knee_circles": lambda: [K(half_squat(0.5), {"pelvis": (40, 0, 0), "aim": sym(lambda lat: limb(V(D, 1, F, .6), sec=F))})],
        "frog_supine": lambda: [P("supine", legs={"flex": 100, "abd": 45, "rot": 45, "knee": 110}, aim=SUP_ARMS_FLOOR),
                                P("supine", legs={"flex": 60, "abd": 25, "rot": 40, "knee": 10, "ankle": 20}, aim=SUP_ARMS_FLOOR)],
        "footwork": lambda: [P("supine", legs={"flex": 120, "knee": 110, "ankle": 30}, aim=SUP_ARMS_FLOOR), P("supine", legs={"flex": 60, "ankle": 20}, aim=SUP_ARMS_FLOOR)],
        "eight_point": lambda: [P(aim=sym(lambda lat: limb(V(lat, 1, B, .3, U, .2), sec=F)))],
        "diamond_lift": lambda: [P("prone", legs={"abd": 30, "rot": 45, "knee": 70}, aim=sym(lambda lat: {"arm_upper": (V(lat, 1, F, .5), D), "arm_lower": (V(-lat, 1, F, .6), D), "hand": (V(-lat), D)})),
                                 P("prone", legs={"flex": -15, "abd": 30, "rot": 45, "knee": 70}, spine=(-10, 0, 0), aim=sym(lambda lat: {"arm_upper": (V(lat, 1, F, .5), D), "arm_lower": (V(-lat, 1, F, .6), D), "hand": (V(-lat), D)}))],
        "diamond_crunch": lambda: [P("supine", legs={"flex": 90, "abd": 30, "rot": 45, "knee": 70}, aim=behind_head_aim()),
                                   P("supine", spine=(30, 0, 0), legs={"flex": 100, "abd": 30, "rot": 45, "knee": 70}, aim=behind_head_aim())],
    }
    return S.get(name)


# ===================================================================== classifier
def Ukeys(key, i=None):
    ks = list(USER[key][1])
    return [ks[i]] if i is not None else ks


def St(name):
    return lambda c: stretch(name)()


def Yg(name):
    return lambda c: yoga(name)()


SA = f_seated_arms
SS = f_seated_spine

# (category or "", name regex, builder(c), label). First match wins; "" matches every category.
RULES = [
    # --- seated and wheelchair ---
    ("", r"deep_breathing_low", lambda c: SA(c, "low"), "seated_low"),
    ("", r"deep_breathing_mid", lambda c: SA(c, "mid"), "seated_mid"),
    ("", r"deep_breathing_high", lambda c: SA(c, "high"), "seated_high"),
    ("", r"deep_breathing", lambda c: SA(c, "raise"), "seated_raise"),
    ("", r"overhead_prayer", lambda c: SA(c, "prayer_up"), "seated_prayer_up"),
    ("", r"^prayer_wheelchair", lambda c: SA(c, "prayer"), "seated_prayer"),
    ("", r"cactus", lambda c: SA(c, "cactus"), "seated_cactus"),
    ("", r"(breathing_punches.*side|punch_sideways)", lambda c: f_punch(c, "sideways", "double" in c.name), "punch_side"),
    ("", r"(punch.*_up_|punch_up|breathing_punches_overhead_wheelchair|breathing_punches_overhead_side)", lambda c: f_punch(c, "up", "double" in c.name), "punch_up"),
    ("", r"(punch.*down|overhead_and_down)", lambda c: f_punch(c, "down", "double" in c.name), "punch_down"),
    ("", r"(punch|jabs|bob_and_weave)", lambda c: f_bob_weave(c) if "bob" in c.name else f_punch(c, "forward", "double" in c.name), "punch"),
    ("", r"left_hand_back", lambda c: SS(c, "hand_back"), "seated_hand_back"),
    ("", r"triangle_wheelchair", lambda c: SS(c, "triangle"), "seated_triangle"),
    ("", r"thread_the_needle_wheelchair", lambda c: SS(c, "thread"), "seated_thread"),
    ("", r"neck_flexion", lambda c: SS(c, "neck_flex"), "seated_neck_flex"),
    ("", r"neck_lateral", lambda c: SS(c, "neck_lat"), "seated_neck_lat"),
    ("", r"neck_rotations_wheelchair", lambda c: SS(c, "neck_rot"), "seated_neck_rot"),
    ("", r"(spine_flexion|seated_cat_cow)", lambda c: SS(c, "flex"), "seated_cat_cow"),
    ("", r"(spine_rotation|core_twists_wheelchair|seated_trunk_rotations)", lambda c: SS(c, "rotation"), "seated_rotation"),
    ("", r"(spine_lateral|side_bend_.*wheelchair|seated_side_bend)", lambda c: SS(c, "lateral"), "seated_side_bend"),
    ("", r"(alternative_skiing|sit_ski|pole_paddle|pole_dd)", lambda c: SA(c, "ski"), "seated_ski"),
    ("", r"(reach_forward_wheelchair|arm_extend_wheelchair)", lambda c: SA(c, "reach"), "seated_reach"),
    ("", r"reverse_warrior_wheelchair", lambda c: SS(c, "reverse_warrior"), "seated_reverse_warrior"),
    ("", r"warrior_wheelchair", lambda c: SS(c, "warrior"), "seated_warrior"),
    ("", r"(forward_fold.*wheelchair|forward_bend_wheelchair)", lambda c: SS(c, "forward_fold"), "seated_fold"),
    ("", r"toe_touch_wheelchair", lambda c: SS(c, "toe_touch"), "seated_toe_touch"),
    ("", r"(arm_raise_wheelchair|pole_straight_arm_overhead|pole_double_arm_overhead)", lambda c: SA(c, "raise"), "seated_raise"),
    ("", r"chest_expand", lambda c: SA(c, "chest_expand"), "seated_chest_expand"),
    ("", r"butterfly_arms", lambda c: SA(c, "butterfly"), "seated_butterfly"),
    ("", r"pole_rainbow", lambda c: SA(c, "rainbow"), "seated_rainbow"),
    ("", r"handcycle", lambda c: f_handcycle(c), "seated_handcycle"),
    ("", r"sitting_jacks", lambda c: f_jumping_jack(c, "seated"), "seated_jacks"),
    ("", r"(arm_circles_(low|mid|high)|seated_arm_circles)", lambda c: [K(chair(), {"aim": T_ARMS}), K(chair(), {"aim": sym(lambda lat: limb(V(lat, 1, U, .2), sec=F))})], "seated_arm_circles"),
    ("", r"burpee_wheelchair", lambda c: f_burpee(c, "seated"), "seated_burpee"),
    ("", r"core_twists|trunk_rotations$", lambda c: St("twist_standing")(c), "twist_standing"),
    # --- user-pose matches by name ---
    ("", r"downward_facing_dog_to_cobra", lambda c: Ukeys("pose/114"), "down_dog_to_cobra"),
    ("", r"(cat_cow|cat_camel)", lambda c: Ukeys("core/51"), "cat_cow"),
    ("", r"(arm_and_leg_extension_on_knees|opposite_arm_and_leg_balance|kneeling_superman|bird_dog)", lambda c: Ukeys("core/48"), "bird_dog"),
    ("", r"^criss_cross", lambda c: Ukeys("core/53"), "criss_cross"),
    ("", r"double_leg_stretch", lambda c: Ukeys("core/55"), "double_leg_stretch"),
    ("", r"^roll_over", lambda c: Ukeys("core/60"), "roll_over"),
    ("", r"(^roll_up|pilates_roll_up|roll_up_with|neck_pull|three_part_roll_down)", lambda c: Ukeys("core/61"), "roll_up"),
    ("", r"single_leg_stretch", lambda c: Ukeys("core/67"), "single_leg_stretch"),
    ("", r"^swan$", lambda c: Ukeys("core/69"), "swan"),
    ("", r"^swimming$", lambda c: Ukeys("core/70"), "swimming"),
    ("", r"the_hundred", lambda c: Ukeys("core/72"), "hundred"),
    ("", r"(^cobra$|stretch_cobra)", lambda c: Ukeys("hyperextension/38"), "cobra"),
    ("", r"arm_stretch", lambda c: Ukeys("move/2"), "arm_stretch"),
    ("", r"(piriformis|glutes_stretch|forward_glutes)", lambda c: Ukeys("move/31"), "piriformis"),
    ("", r"plank_up_downs", lambda c: Ukeys("move/34"), "plank_up_down"),
    ("", r"^roll_down", lambda c: Ukeys("move/38"), "roll_down"),
    ("", r"spine_stretch_forward", lambda c: Ukeys("move/48"), "spine_stretch"),
    ("", r"squat_open_arm_twist", lambda c: Ukeys("pose/62"), "squat_twist"),
    ("", r"(neck_rotations|neck_circles)", lambda c: Ukeys("warm_up/13"), "neck_rotations"),
    ("", r"(three_way_ankle|ankle_lunges)", lambda c: Ukeys("warm_up/73"), "ankle_mob"),
    ("", r"(ninety_ninety|stretch_90_90|90_90)", lambda c: Ukeys("warm_up/74"), "ninety_ninety"),
    ("", r"chest_openers", lambda c: Ukeys("warm_up/83"), "chest_openers"),
    ("", r"(stretch_childs_pose|^childs$)", lambda c: Ukeys("pose/11"), "childs"),
    ("", r"(stretch_abs|^up_dog$)", lambda c: Ukeys("pose/80"), "up_dog"),
    ("", r"(lunge_with_spinal_twist|low_lunge_twist)", lambda c: Ukeys("pose/40"), "lunge_twist"),
    ("", r"(lunging_hip_flexor|psoas_lunge|low_lunge_with_isometric)", lambda c: Ukeys("lunge/30"), "low_lunge_hold"),
    ("", r"(^lizard$|elbow_to_foot_lunge)", lambda c: Ukeys("pose/37"), "lizard"),
    ("", r"standing_wide_leg_forward_bend_with_twist|bent_over_reach_to_sky", lambda c: Ukeys("pose/72"), "wide_fold_twist"),
    ("", r"(seated_easy_twist|seated_twist|spinal_twist_stretch|^spinal_twist$)", lambda c: Ukeys("pose/55"), "seated_twist"),
    ("", r"seated_long_leg_forward_bend|^stretch_hamstring$", lambda c: Ukeys("pose/56"), "seated_fold"),
    ("", r"extended_side_angle", lambda c: Ukeys("pose/25"), "extended_side_angle"),
    ("", r"seated_easy_side_bend", lambda c: Ukeys("pose/54"), "seated_side_bend_yoga"),
    ("", r"^dolphin$", lambda c: Ukeys("pose/17"), "dolphin"),
    ("", r"(downward_facing_dog$|^down_dog$)", lambda c: Ukeys("pose/21"), "down_dog"),
    ("", r"extended_puppy", lambda c: Ukeys("pose/24"), "puppy"),
    # --- routing fixes (before the general rules) ---
    ("", r"body_bar_oblique|oblique_twist", f_side_bend, "side_bend"),
    ("", r"side_abs_leg_lift|side_lying_leg", lambda c: f_side_lying_leg(c), "side_lying_leg"),
    ("", r"deadlift.*row|rdl.*row", lambda c: f_deadlift(c, "single" if c.single_leg else "rdl") + [K(hinge(65, 25), {"aim": ROW_TOP})], "deadlift_row"),
    # --- planks ---
    ("", r"plank_with_arm_raise", lambda c: Ukeys("plank/54"), "plank_arm_raise"),
    ("", r"plank_with_arm_variations", lambda c: Ukeys("plank/132"), "plank_arm_variations"),
    ("", r"(plank_with_leg_lift|plank_leg_lift|high_plank_leg_lifts|single_leg_flex_and_extend|single_leg_elevated_feet_plank)", lambda c: Ukeys("plank/133"), "plank_leg_lift"),
    ("", r"(plank_knee_twist|plank_with_knee_to_elbow|plank_with_oblique|passe_twist|plank_knee_tuck)", lambda c: Ukeys("plank/58"), "plank_knee_to_elbow"),
    ("", r"plank_to_stand", lambda c: [plank_high(), K(squat_pose(1.1), {"aim": SQUAT_HANDS}), P(aim=DOWN)], "plank_to_stand"),
    ("", r"kneeling_plank", lambda c: [P("prone", aim={**FOREARM, **legs_both({"leg_lower": (B, D), "foot": (B, D)})}, contacts=("forearms", "knees"))], "kneeling_plank"),
    ("", r"spiderman_plank", f_spiderman_plank, "spiderman_plank"),
    ("", r"(two_point_plank|wide_stance_plank)", f_two_point, "two_point_plank"),
    ("", r"(sprawl)", lambda c: [plank_high(), Ukeys("pose/80")[0]], "sprawl"),
    ("", r"(straight_arm_plank|extended_plank|45_degree_plank|elevated_feet_plank|feet_on_swiss_ball|swiss_ball_plank_with_feet)", lambda c: [plank_high()], "plank_high"),
    ("", r"(90_degree_static_hold|elevator_abs|swiss_ball_plank|^plank$)", lambda c: [plank_fore()], "plank"),
    # --- one-offs ---
    ("", r"cardio_core_crawl", lambda c: f_bear(c, crawl=True), "bear_crawl"),
    ("", r"ski_moguls", f_ski_moguls, "ski_moguls"),
    ("", r"^elliptical$", f_stepper, "stepper"),
    ("", r"^spinning$", f_bike, "bike"),
    ("", r"^ab_twist$", lambda c: St("twist_standing")(c), "twist_standing"),
    ("", r"alternating_plate_reach", lambda c: f_crunch(c, twist=25), "crunch_twist"),
    ("", r"(cable_core_press|pallof)", f_chest_press_standing, "chest_press_standing"),
    ("", r"(crescent_circle|reclining_circle|seated_leg_u)", lambda c: [f_vsit(c)[0], mirror(P("supine", pelvis=(50, 0, 0), spine=(0, 0, 15), aim={**legs_both({"leg_upper": (V(B, 1, U, 1.0, LT, .4), U), "leg_lower": (V(B, 1, U, 1.0, LT, .4), U), "foot": (V(B, 1, U, 1.3), U)}), **sym(lambda lat: limb(V(D, 1, lat, .3), sec=B))}))], "v_sit_circle"),
    ("", r"open_knee_tucks", lambda c: [f_vsit(c, tuck=True)[0], f_vsit(c)[0]], "knee_tucks"),
    ("", r"triangle_hip_press", lambda c: f_bridge(c), "bridge"),
    ("", r"bicep_curl_with_leg_extension", lambda c: f_curl(C(c.cat, "single_leg_curl")), "curl_single_leg"),
    ("", r"^rolling$", lambda c: [P("supine", pelvis=(45, 0, 0), spine=(25, 0, 0), neck=(20, 0, 0), aim={**legs_both({"leg_upper": (V(U, 1, F, .2), F), "leg_lower": (V(B, 1, D, .3), U), "foot": (B, U)}), **sym(lambda lat: limb(V(B, 1, U, .2, lat, .2), sec=U))})], "rolling"),
    ("", r"snake_twist", lambda c: f_side_plank(c, "t"), "plank_to_side"),
    ("", r"frog_press", lambda c: St("frog_supine")(c), "stretch_frog_supine"),
    ("", r"hip_crossover", lambda c: St("windshield")(c), "stretch_windshield"),
    ("", r"step_climb", lambda c: f_crunch(c, legs="up"), "crunch_legs_up"),
    ("", r"base_rotations", lambda c: [P("prone", spine=(-15, 0, 25), aim=sym(lambda lat: limb(V(lat, 1, U, .2), sec=D))), mirror(P("prone", spine=(-15, 0, 25), aim=sym(lambda lat: limb(V(lat, 1, U, .2), sec=D))))], "prone_rotation"),
    ("hyperextension", r"^kicks$", lambda c: [P("prone", LL={"flex": -20, "ankle": 70}, RL={"ankle": 70}, aim=FOREARM), mirror(P("prone", LL={"flex": -20, "ankle": 70}, RL={"ankle": 70}, aim=FOREARM))], "prone_kicks"),
    ("hyperextension", r"^knee_raises$", lambda c: [P("prone", aim=FOREARM), P("prone", legs={"flex": -15, "knee": 90}, aim=FOREARM)], "reverse_hyper"),
    ("", r"supine_floor_barre", lambda c: f_bridge(c), "bridge"),
    ("", r"lying_dumbbell_raise", f_rear_raise, "rear_raise"),
    ("", r"shaving_the_head", lambda c: St("shaving_head")(c), "stretch_shaving_head"),
    ("", r"split_stance_extension", f_good_morning, "good_morning"),
    ("", r"mermaid", lambda c: yoga("mermaid")(), "pose_mermaid"),
    ("", r"prayer_mudra", lambda c: yoga("prayer_mudra")(), "pose_prayer_mudra"),
    ("", r"standing_single_leg_forward_bend", lambda c: yoga("standing_single_leg_forward_bend")(), "pose_standing_single_leg_forward_bend"),
    ("", r"hang_pull", lambda c: f_olympic(c, "high_pull"), "high_pull"),
    ("sandbag", r"shouldering", lambda c: f_olympic(c, "clean"), "clean"),
    ("sandbag", r"shoveling", lambda c: f_chop(c, reverse=True), "chop"),
    ("sandbag", r"sprint", lambda c: f_run(c, 1.3, 12), "sprint"),
    # --- whole body ---
    ("", r"burpee.*pull_up", lambda c: f_burpee(c, "pullup"), "burpee_pullup"),
    ("", r"burpee.*box", lambda c: f_burpee(c, "box"), "burpee_box"),
    ("", r"high_pull_burpee", lambda c: f_burpee(c, "high_pull"), "burpee_high_pull"),
    ("", r"squat_plank_push_up", lambda c: f_burpee(c, "pushup"), "burpee_pushup"),
    ("", r"squat_thrust", lambda c: f_burpee(c, "thrust"), "squat_thrust"),
    ("", r"burpee", lambda c: f_burpee(c), "burpee"),
    ("", r"man_maker", f_man_maker, "man_maker"),
    ("", r"(turkish_get_up|get_up_sit_up)", f_turkish_get_up, "turkish_get_up"),
    ("", r"windmill", f_windmill, "windmill"),
    ("", r"(inchworm|walkout|inching)", f_inchworm, "inchworm"),
    ("", r"bear_crawl", lambda c: f_bear(c, crawl=True), "bear_crawl"),
    ("", r"mountain_climber|plank_knee_pulls|towel_plank_and_knee|plank_knee_crosses", lambda c: f_mountain_climber(c, "cross" in c.name), "mountain_climber"),
    ("", r"plank_jack|plank_pike_jumps", f_plank_jacks, "plank_jacks"),
    ("", r"(side_plank.*(leg_lift|lift)|side_kick_plank|side_plank_leg_lifts)", lambda c: f_side_plank(c, "leg_lift"), "side_plank_leg_lift"),
    ("", r"side_plank.*(knee_tuck|knee_circle)", lambda c: f_side_plank(c, "tuck"), "side_plank_tuck"),
    ("", r"side_plank.*(reach_under|row)", lambda c: f_side_plank(c, "reach"), "side_plank_reach"),
    ("", r"kneeling_side_plank", lambda c: f_side_plank(c, "kneel"), "side_plank_kneel"),
    ("", r"(rolling_side_plank|lateral_roll|three_way_plank|t_stabilization|side_plank_to_plank|inchworm_to_side|turkish_get_up_to_side|t_rotation)", lambda c: f_side_plank(c, "t"), "plank_to_side"),
    ("", r"(side_plank|side_boat)", lambda c: f_side_plank(c, "straight" if "straight" in c.name else "std"), "side_plank"),
    ("", r"(reverse_plank|upward_facing_plank|lying_reverse_plank)", lambda c: f_reverse_plank(c, "leg" in c.name), "reverse_plank"),
    # --- pushing ---
    ("", r"push_up_plus", lambda c: [plank_high()], "plank_high"),
    ("", r"(wall_push|incline_push|wall_walk_push)", lambda c: f_push_up(c, "wall"), "push_up_incline"),
    ("", r"(handstand_push|wall_walk|handstand)", lambda c: f_push_up(c, "handstand"), "handstand_push_up"),
    ("", r"(pike_push|floor_inverted_shoulder_press|inverted_shoulder_press)", lambda c: f_push_up(c, "pike"), "pike_push_up"),
    ("", r"(hindu_push|judo_push|dive_bomb)", lambda c: f_push_up(c, "hindu"), "hindu_push_up"),
    ("", r"t_push_up", lambda c: f_push_up(c, "t"), "t_push_up"),
    ("", r"spiderman_push", lambda c: f_push_up(c, "spider"), "spiderman_push_up"),
    ("", r"(clapping_push|explosive.*push|plyo.*push|dynamic_push)", lambda c: f_push_up(c, "clap"), "clap_push_up"),
    ("", r"push_up_and_row", lambda c: f_push_up(c, "row"), "push_up_row"),
    ("", r"(shoulder_tapping|shoulder_touch)", lambda c: f_push_up(c, "tap"), "push_up_tap"),
    ("", r"(one_arm_push|single_arm_.*push_up)", lambda c: f_push_up(c, "one_arm"), "one_arm_push_up"),
    ("", r"(kneeling_push|modified_push)", lambda c: f_push_up(c, "kneel"), "kneeling_push_up"),
    ("", r"(push_?up|pushups|^push_ups$)", lambda c: f_push_up(c), "push_up"),
    ("", r"(bench_dip|tabletop_dip|single_leg_bench_dip|single_leg_dip)", lambda c: f_dip(c, bench=True), "bench_dip"),
    ("", r"(^dip$|_dip$|_dip_|ring_dip|body_weight_dip|incline_dip)", lambda c: f_dip(c), "dip"),
    # --- pulling ---
    ("", r"muscle_up", lambda c: f_pull_up(c, muscle=True), "muscle_up"),
    ("", r"rope_climb", f_rope_climb, "rope_climb"),
    ("", r"l_pull_up", lambda c: f_pull_up(c, legs90=True), "l_pull_up"),
    ("", r"(pull_?up|chin_?up|hanging_hurdle|towel_pull|dead_hang)", lambda c: f_pull_up(c), "pull_up"),
    ("", r"(straight_arm_pull_?down|straight_arm_pulldown)", lambda c: f_pulldown(c, straight=True), "straight_arm_pulldown"),
    ("", r"(pull_?down|lat_?pull|latpull)", f_pulldown, "pulldown"),
    ("", r"standing_cable_pullover", lambda c: f_pulldown(c, straight=True), "straight_arm_pulldown"),
    ("", r"pullover", f_pullover, "pullover"),
    ("", r"face_pull", f_face_pull, "face_pull"),
    ("", r"pull_apart", f_pull_apart, "pull_apart"),
    ("", r"(inverted_row|ring_row|suspended_row|elevated_ring_row|decline_ring_row|^inverted_row)", f_inverted_row, "inverted_row"),
    ("", r"(renegade_row|plank_.*row)", f_renegade_row, "renegade_row"),
    ("", r"upright_row", f_upright_row, "upright_row"),
    ("", r"(rowing_machine|^indoor_row$|calorie_row|rowing_1|rowing_2|^row$)", f_rower, "rower"),
    ("", r"(cable_row_standing|standing.*row)", lambda c: f_row_seated(c, standing=True), "row_standing"),
    ("", r"(seated.*row|cable_row|rope_handle_cable|v_grip_cable|underhand_grip_cable|wide_grip_seated)", f_row_seated, "row_seated"),
    ("", r"high_pull", lambda c: f_olympic(c, "high_pull") if "sumo" not in c.name else f_deadlift(c, "sumo"), "high_pull"),
    ("", r"_row|^row_", f_row_bent, "row_bent"),
    # --- olympic ---
    ("", r"clean_and_press", lambda c: f_olympic(c, "clean_press"), "clean_press"),
    ("", r"(clean_and_jerk|clean_and_push_press|clean_and_strict_press)", lambda c: f_olympic(c, "clean_jerk"), "clean_jerk"),
    ("", r"snatch", lambda c: f_olympic(c, "snatch"), "snatch"),
    ("", r"clean", lambda c: f_olympic(c, "clean"), "clean"),
    ("", r"(squat_to_push_press|front_squat_to_push|thruster|wall_ball|squat_and_press|squat_to_press|sumo_squat_to_stand)", lambda c: f_squat(c, press=True), "thruster"),
    ("", r"(jerk|push_press)", lambda c: f_olympic(c, "jerk") if "jerk" in c.name else f_press(c, dip=True), "jerk"),
    ("", r"(kettlebell_swing|_swing$|swing_overhead|kbs_overhead|american_swing|step_out_swing|one_arm_swing|swing_with_flip)", lambda c: f_kb_swing(c, overhead=bool(re.search(r"overhead|american|kbs", c.name))), "kb_swing"),
    # --- hinge, bridge, legs ---
    ("", r"good_morning", f_good_morning, "good_morning"),
    ("", r"(single_leg.*(deadlift|rdl)|one_leg.*deadlift|single_leg_rdl)", lambda c: f_deadlift(c, "single"), "single_leg_deadlift"),
    ("", r"sumo_deadlift", lambda c: f_deadlift(c, "sumo"), "sumo_deadlift"),
    ("", r"(straight_leg_deadlift|romanian|rdl|stiff)", lambda c: f_deadlift(c, "rdl"), "rdl"),
    ("", r"rack_pull", lambda c: f_deadlift(c, "rack"), "rack_pull"),
    ("", r"(deadlift|floor_to_shelf)", lambda c: f_deadlift(c), "deadlift"),
    ("", r"(single_leg.*(hip_raise|bridge)|one_leg_bridge|single_leg_swiss_ball_hip_raise)", lambda c: f_bridge(c, single=True), "bridge_single"),
    ("", r"marching_hip_raise", lambda c: f_bridge(c, march=True), "bridge_march"),
    ("", r"(bridge_with_leg_extension)", lambda c: f_bridge(c, single=True), "bridge_single"),
    ("", r"(hip_thrust|glute_bridge|hip_raise|bridge|tailbone_curl|pelvic_clock|clam_bridge)", lambda c: f_bridge(c), "bridge"),
    ("", r"(calf_raise|toe_raise|releve|calf_raises)", f_calf_raise, "calf_raise"),
    ("", r"(leg_curl|hamstring_curl)", f_leg_curl, "leg_curl"),
    ("", r"^(leg_extension|banded_leg_extension)$", f_leg_ext, "leg_extension"),
    ("", r"leg_press", lambda c: St("footwork")(c), "leg_press"),
    ("", r"(step_?up|stepover|box_step|bench_get_up)", f_step_up, "step_up"),
    ("", r"(pistol|one_legged_squat|single_leg_squat|single_leg_bench_squat|elevated_single_leg|partial_single_leg|figure_four_squat)", f_pistol, "pistol"),
    ("", r"(bulgarian|back_foot_elevated|rear_foot_elevated)", lambda c: f_lunge(c, "bulgarian"), "bulgarian"),
    ("", r"(split_squat|lunge_squat|staggered_squat)", lambda c: f_lunge(c, "split"), "split_squat"),
    ("", r"(side_lunge|lateral_lunge|side_to_side_lunge|low_side_to_side|shifting_side|sliding_lateral|stretch_side_lunge|lateral_slide)", lambda c: f_lunge(c, "side"), "side_lunge"),
    ("", r"(curtsy|crossover_lunge|crossover_dumbbell|reverse_dumbbell_crossover)", lambda c: f_lunge(c, "curtsy"), "curtsy_lunge"),
    ("", r"(jump_lunge|jump.*lunge|split_jump|scissor|siff_jump|switch_lunge|lunge_jump)", lambda c: f_lunge(c, "jump"), "jump_lunge"),
    ("", r"lunge", lambda c: f_lunge(c), "lunge"),
    ("", r"(box_jump|depth_jump|tuck_jump|squat_jump_onto_box|high_box)", f_box_jump, "box_jump"),
    ("", r"(jump_squat|squat_jump|star_jump|squats_in_and_out|squat_jumps_in|lateral_plyo_squats)", lambda c: f_squat(c, jump=True), "squat_jump"),
    ("", r"(squat|plie|wall_sit|sit_to_stand)", lambda c: f_squat(c), "squat"),
    # --- jumps, cardio, locomotion ---
    ("", r"split_jacks|split_jack", lambda c: f_jumping_jack(c, "split"), "split_jacks"),
    ("", r"squat_jacks", lambda c: f_jumping_jack(c, "squat"), "squat_jacks"),
    ("", r"(jumping_jack|^jacks$|star_jump)", lambda c: f_jumping_jack(c), "jumping_jacks"),
    ("", r"(jump_rope|double_under|triple_under)", lambda c: f_jump_rope(c, "cross" in c.name), "jump_rope"),
    ("", r"(skater|lateral_leap|side_to_side_shuffle|shuffle|lateral_duck)", f_skater, "skater"),
    ("", r"(knee_strike)", lambda c: f_knee_strike(c, "cross" in c.name), "knee_strike"),
    ("", r"hammer_slam", lambda c: f_slam(c, "hammer"), "hammer_slam"),
    ("", r"(slam)", lambda c: f_slam(c), "slam"),
    ("", r"(side_throw|side_to_side_medicine|grappler|hip_toss)", lambda c: f_slam(c, "side_throw"), "side_throw"),
    ("", r"chest_pass", lambda c: f_slam(c, "chest_pass"), "chest_pass"),
    ("", r"(throw|toss)", lambda c: f_slam(c, "throw"), "throw"),
    ("", r"(walking_high_knees|high_knee|knee_drive)", f_high_knees, "high_knees"),
    ("", r"(marching|march)", lambda c: f_high_knees(c, "march"), "march"),
    ("", r"butt_kick", lambda c: f_high_knees(c, "butt"), "butt_kicks"),
    ("", r"(carry|farmers|waiter|bar_holds|hex_dumbbell_hold|overhead_walk|front_carry)", lambda c: f_carry(c, "overhead" if "overhead" in c.name else "waiter" if "waiter" in c.name else "front" if "front" in c.name else "down", "toes" in c.name), "carry"),
    ("", r"cycling_russian_twist", f_russian_twist, "russian_twist"),
    # --- arms ---
    ("", r"wrist_curl", f_wrist_curl, "wrist_curl"),
    ("", r"(curl(?!_up)(?!_and)|hammer)", lambda c: f_curl(c) if not re.search(r"leg|hamstring|tailbone|arch_and|dead_curl|reverse_curl_and", c.name) else None, "curl"),
    ("", r"(close_grip.*bench|close_grip.*press|triceps_press_to_close)", lambda c: f_bench_press(c, close=True), "bench_press_close"),
    ("", r"(bench_press|floor_press|partial_lockout|board_bench|kettlebell_chest_press|swiss_ball_dumbbell_chest_press|alternating_dumbbell_chest_press)", lambda c: f_bench_press(c), "bench_press"),
    ("", r"chest_press", f_chest_press_standing, "chest_press_standing"),
    ("", r"(shoulder_press|overhead_press|military|strict_press|arnold|smith_machine_overhead|overhead_dumbbell_press|overhead_barbell_press|dead_curl_press|curl_to_press|curl_to_overhead|overhead_press_wheelchair|step_up_and_press)", lambda c: f_press(c, split="split" in c.name), "press"),
    ("", r"(rear_delt|reverse_flye|reverse_fly|rear_flye|rear_lateral|bent_over_lateral|kneeling_rear)", f_rear_raise, "rear_raise"),
    ("", r"(cable_crossover|standing_cable_re|chest_fly_incline_wheelchair|chest_fly_decline)", lambda c: f_flye(c, standing=True), "flye_standing"),
    ("", r"(flye|fly$|_fly_|hug_a_tree$)", lambda c: f_flye(c), "flye"),
    ("", r"(front_raise|plate_raise|weight_plate_front|shoulder_flexion)", lambda c: f_raise(c, "front"), "front_raise"),
    ("", r"v_raise", lambda c: f_raise(c, "v"), "v_raise"),
    ("", r"diagonal_raise", lambda c: f_raise(c, "diagonal"), "diagonal_raise"),
    ("", r"scaption", lambda c: f_raise(c, "scaption"), "scaption"),
    ("", r"(lateral_raise|side_raise|shoulder_abduction|combo_shoulder_raise|standing_lift)", lambda c: f_raise(c, "lateral"), "lateral_raise"),
    ("", r"(shrug|scapular_retraction|serratus)", f_shrug, "shrug"),
    ("", r"(skull|lying.*triceps|lying_ez_bar|reclining_triceps|triceps_extension_on_floor|swiss_ball.*lying_triceps|lying_triceps)", f_skull_crusher, "skull_crusher"),
    ("", r"(pressdown|press_down|triceps_press$|reverse_grip_triceps)", f_pressdown, "pressdown"),
    ("", r"(kickback)", f_kickback, "kickback"),
    ("", r"(triceps?_extension|overhead.*extension)", f_triceps_overhead, "triceps_overhead"),
    ("", r"(external_rotation_at_90|90_degree_cable_external|bent_arm_lateral_raise_and_external|standing_l_raise|45_degree_cable_external)", lambda c: f_ext_rotation(c, at90=True), "ext_rotation_90"),
    ("", r"internal_rotation", lambda c: f_ext_rotation(c, internal=True), "int_rotation"),
    ("", r"external_rotation", f_ext_rotation, "ext_rotation"),
    ("", r"_i_raise", lambda c: f_ity(c, "i"), "i_raise"),
    ("", r"_t_raise", lambda c: f_ity(c, "t"), "t_raise"),
    ("", r"_y_raise", lambda c: f_ity(c, "y"), "y_raise"),
    ("", r"_w_raise", lambda c: f_ity(c, "w"), "w_raise"),
    ("", r"_l_raise", lambda c: f_ity(c, "l"), "l_raise"),
    ("", r"(arm_circles|shoulder_circles|arm_rotations|shoulder_rolls)", f_arm_circles, "arm_circles"),
    ("", r"(around_the_world|shoulder_sweeps)", f_around_world, "around_world"),
    ("", r"shoulder_extension", f_shoulder_ext, "shoulder_extension"),
    ("", r"(wall_slide|wall_crawl)", f_wall_slide, "wall_slide"),
    # --- core ---
    ("", r"russian_twist", f_russian_twist, "russian_twist"),
    ("", r"(bicycle|criss_cross)", f_bicycle, "bicycle"),
    ("", r"(reverse_crunch|reverse_curl|cross_leg_reverse)", f_reverse_crunch, "reverse_crunch"),
    ("", r"hanging_knee_raise_oblique", lambda c: f_hanging_raise(c, knees=True, twist=30), "hanging_knee_oblique"),
    ("", r"(hanging_knee_raise|knees_to_elbow|toes_to_elbows)", lambda c: f_hanging_raise(c, knees=True), "hanging_knee_raise"),
    ("", r"(toes_to_bar)", lambda c: f_hanging_raise(c, toes=True), "toes_to_bar"),
    ("", r"(hanging.*leg_raise|hanging_l_sit|ring_l_sit)", lambda c: f_hanging_raise(c), "hanging_leg_raise"),
    ("", r"(single_leg_lowering|single_leg_circles|leg_circles)", lambda c: f_leg_lower(c, single=True), "single_leg_lower"),
    ("", r"(leg_raise|leg_lowering|leg_drops|leg_levers|leg_extensions|lower_lift|abdominal_leg_rotations|corkscrew|leg_lift$)", lambda c: f_leg_lower(c), "leg_lower"),
    ("", r"(flutter|scissors)", f_flutter, "flutter"),
    ("", r"hollow", lambda c: f_hollow(c, rock="rock" in c.name or "roll" in c.name), "hollow"),
    ("", r"(dead_?bug|knee_folds)", f_dead_bug, "dead_bug"),
    ("", r"butterfly_sit", lambda c: f_situp(c, "butterfly"), "butterfly_situp"),
    ("", r"(v_up|^teaser|the_teaser|x_abs|v_leg_crunch)", lambda c: f_situp(c, "vup"), "v_up"),
    ("", r"(v_sit|^boat$|u_boat)", lambda c: f_vsit(c), "v_sit"),
    ("", r"(sit_?up|ghd_sit)", lambda c: f_situp(c), "sit_up"),
    ("", r"(jackknife|swiss_ball_pike|plank_pike|^pike$)", f_jackknife, "jackknife"),
    ("", r"(oblique|side_crunch|cross_crunch|elbow_to_knee|side_to_side_crunch|skater_crunch|crunch_chop)", lambda c: f_crunch(c, twist=25), "crunch_twist"),
    ("", r"(raised_legs_crunch|double_crunch)", lambda c: f_crunch(c, legs="table"), "crunch_table"),
    ("", r"straight_leg_crunch|leg_climb", lambda c: f_crunch(c, legs="up"), "crunch_legs_up"),
    ("", r"(standing.*crunch|standing_side_crunch)", lambda c: [P(aim=behind_head_aim() if False else BEHIND_HEAD), P(RL=lw(0, 85, 0), spine=(20, 0, -20), aim=BEHIND_HEAD)], "standing_crunch"),
    ("", r"kneeling.*crunch", lambda c: [K(kneel(), {"aim": sym(lambda lat: {"arm_upper": (V(U, 1, F, .3, lat, .3), F), "arm_lower": (V(-lat, 1, B, .2, D, .2), F), "hand": (V(-lat), F)})}),
                                          K(kneel(), {"pelvis": (25, 0, 0), "spine": (50, 0, 0), "aim": sym(lambda lat: {"arm_upper": (V(F, 1, D, .6, lat, .3), F), "arm_lower": (V(-lat, 1, B, .3, U, .2), F), "hand": (V(-lat), F)})})], "kneeling_crunch"),
    ("", r"crunch|curl_up", lambda c: f_crunch(c), "crunch"),
    ("", r"(superman.*swiss|superman_from_floor|superman$)", f_superman, "superman"),
    ("", r"(opposite_arm_and_leg|alternating_superman|back_extension_with_opposite)", lambda c: f_superman(c, alt=True), "superman_alt"),
    ("", r"(reverse_leg_raise|reverse_hip_raise|bent_knee_reverse_hyper|single_leg_back_extension)", lambda c: [P("prone", aim=FOREARM), P("prone", legs={"flex": -25, "ankle": 60}, aim=FOREARM)], "reverse_hyper"),
    ("", r"(back_extension|hyperextension|spine_extension|ghd_back)", lambda c: f_superman(c, hands="head"), "back_extension"),
    ("", r"(side_bend|oblique_twist|body_bar_oblique)", f_side_bend, "side_bend"),
    ("", r"(rollout|ab_wheel|slide_out|alternating_slide_out)", lambda c: f_rollout(c, kneeling="barbell" not in c.name or True), "rollout"),
    ("", r"l_sit", lambda c: f_lsit(c), "l_sit"),
    ("", r"front_lever", lambda c: f_hollow(c), "hollow"),
    # --- hips ---
    ("", r"(hip_abduction|leg_abduction|standing_cable_hip_abduction|^abduction)", lambda c: f_hip_abd_standing(c, "abd"), "hip_abduction"),
    ("", r"(adduction)", lambda c: f_hip_abd_standing(c, "add"), "hip_adduction"),
    ("", r"(standing_rear_leg_raise|^hip_extension$|banded_hip_extension|glute_kickback)", lambda c: f_hip_abd_standing(c, "rear"), "hip_extension_standing"),
    ("", r"(clam)", lambda c: f_side_lying_leg(c, "clam"), "clams"),
    ("", r"(inner_thigh)", lambda c: f_side_lying_leg(c, "inner"), "inner_thigh"),
    ("", r"(side_lying_leg_raise|side_abs_leg_lift|external_hip_raise|pretzel_side_kick|side_lying)", lambda c: f_side_lying_leg(c), "side_lying_leg"),
    ("", r"fire_hydrant", lambda c: f_quad_leg(c, "hydrant"), "fire_hydrant"),
    ("", r"donkey_kick", lambda c: f_quad_leg(c, "donkey"), "donkey_kick"),
    ("", r"(quadruped_hip_extension|quadruped_with_leg_lift|quadruped_leg_raise|front_kick_tabletop|incline_rear_leg|hip_extension_and_cross)", lambda c: f_quad_leg(c, "ext"), "quad_leg_ext"),
    ("", r"(lateral_walk|band_walk|lateral_band|monster_walk|side_step)", f_lateral_walk, "lateral_walk"),
    ("", r"(hip_circles)", lambda c: f_hip_circles(c) if c.cat != "warm_up" else stretch("hip_circles_standing")(), "hip_circles"),
    ("", r"(prone_hip_internal)", lambda c: [P("prone", legs={"knee": 90}, aim=FOREARM), P("prone", legs={"knee": 90, "rot": -35}, aim=FOREARM)], "prone_hip_rotation"),
    ("", r"(supine_hip_internal)", lambda c: [P("supine", aim={**legs_both(HOOK), **SUP_ARMS_FLOOR}), P("supine", legs={"rot": -25}, aim=SUP_ARMS_FLOOR)], "supine_hip_rotation"),
    ("", r"(quadruped_rocking)", lambda c: stretch("quad_rock")(), "quad_rock"),
    ("", r"^quadruped$|table_top|all_fours", lambda c: [quad()], "quad"),
    ("", r"chop|woodchop|wood_chop|rotational_lift|pull_through|half_kneeling_rotation", lambda c: f_chop(c, reverse="reverse" in c.name or "lift" in c.name, stability="stability" in c.name), "chop"),
]

# Stretches and warm-ups by keyword (warm_up, move, banded and any stretch_ name).
STRETCH_RULES = [
    (r"neck_tilt", "neck_tilt"), (r"stretch_neck", "neck"), (r"levator", "levator"),
    (r"(ankle_circles|ankle_internal|ankle_stretch|foot_rolling|footwork_on_disc)", "ankle"), (r"ankle_dorsiflexion", "ankle_seated"),
    (r"forward_and_backward_leg_swings|leg_swings", "leg_swing"), (r"side_to_side_leg_swings", "leg_swing_side"),
    (r"groiners", "groiner"), (r"inverted_hamstring", "inverted_hamstring"), (r"reach_roll_and_lift", "reach_roll_lift"),
    (r"scorpion", "scorpion"), (r"sleeper", "sleeper"), (r"(hip_crossover|windshield)", "windshield"), (r"thoracic_rotation", "thoracic_rotation"),
    (r"walking_high_kicks", "high_kick"), (r"walking_knee_hugs", "knee_hug"), (r"walking_leg_cradles", "leg_cradle"),
    (r"biceps_stretch", "biceps"), (r"standing_hamstring", "standing_hamstring"), (r"butterfly", "butterfly"), (r"stretch_calf", "calf"),
    (r"stretch_forearms", "forearms"), (r"front_split|full_split", "front_split"), (r"hip_flexor_and_quad", "hip_flexor_quad"),
    (r"stretch_lat", "lat"), (r"lying_abduction", "lying_abduction"), (r"lying_it_band", "lying_it_band"), (r"(lying_knee_to_chest|back_massage)", "lying_knee_to_chest"),
    (r"(stretch_obliques|stretch_side$|standing_side_stretch|lateral_flexion|standing_side_opener)", "side"), (r"over_under", "over_under"),
    (r"(pectoral|chest_and_shoulder)", "pectoral" if True else "wall_chest"), (r"pigeon|hip_stretch", "pigeon"), (r"stretch_quad", "quad"),
    (r"stretch_shoulder", "shoulder_cross"), (r"(side_split|straddle)", "straddle"), (r"standing_it_band", "standing_it_band"),
    (r"triceps_stretch|stretch_triceps", "triceps"), (r"half_kneeling_arm_rotation", "half_kneel_rotation"), (r"active_frog", "frog"),
    (r"back_roll_foam", "foam_back"), (r"latissimus_dorsi_foam", "foam_lat"), (r"reverse_t_hip", "reverse_t"), (r"upper_back_stretch", "upper_back"),
    (r"(belly_breathing|neutral_pelvic)", "breathing_supine"), (r"ribcage_breathing", "breathing_seated"), (r"throat_lock", "throat_lock"),
    (r"arch_and_curl", "arch_curl"), (r"half_roll_up", "half_roll_up"), (r"knee_circles", "knee_circles"), (r"frog_with_band", "frog_supine"),
    (r"^footwork", "footwork"), (r"eight_point", "eight_point"), (r"diamond_leg_lift", "diamond_lift"), (r"diamond_leg_crunch", "diamond_crunch"),
    (r"(hamstring_stretch|leg_stretch_with)", "hamstring_supine"), (r"hug_a_tree", "hug_tree"), (r"scapular_stabilization", "scap"),
    (r"^saw$", "saw"), (r"shaving_the_head", "shaving_head"), (r"stand_and_hang", "hang"), (r"tick_tock", "tick_tock"),
    (r"(^twist$|trunk_rotations)", "twist_standing"), (r"toe_touch", "toe_touch"), (r"forward_fold", "forward_fold"),
    (r"(seated_hip_stretch)", "butterfly"), (r"(lying_spinal_twist|supine_spinal)", None), (r"stretch_wall_chest", "wall_chest"),
]

CATEGORY_DEFAULT = {
    "banded_exercises": ("squat", lambda c: f_squat(c)), "battle_rope": ("battle_rope", lambda c: f_battle_rope(c)),
    "bench_press": ("bench_press", lambda c: f_bench_press(c)), "bike": ("bike", f_bike), "bike_outdoor": ("bike", f_bike),
    "indoor_bike": ("bike", f_bike), "calf_raise": ("calf_raise", f_calf_raise), "cardio": ("jumping_jacks", lambda c: f_jumping_jack(c)),
    "carry": ("carry", lambda c: f_carry(c)), "chop": ("chop", lambda c: f_chop(c)), "core": ("crunch", lambda c: f_crunch(c)),
    "crunch": ("crunch", lambda c: f_crunch(c)), "curl": ("curl", f_curl), "deadlift": ("deadlift", lambda c: f_deadlift(c)),
    "elliptical": ("stepper", f_stepper), "floor_climb": ("stepper", lambda c: f_stepper(c, handles=False)), "flye": ("flye", lambda c: f_flye(c)),
    "hip_raise": ("bridge", lambda c: f_bridge(c)), "hip_stability": ("hip_abduction", lambda c: f_hip_abd_standing(c)),
    "hip_swing": ("kb_swing", lambda c: f_kb_swing(c)), "hyperextension": ("superman", f_superman), "indoor_row": ("rower", f_rower),
    "ladder": ("high_knees", lambda c: f_run(c, 0.8, knee_up=20)), "lateral_raise": ("lateral_raise", lambda c: f_raise(c)),
    "leg_curl": ("leg_curl", f_leg_curl), "leg_raise": ("leg_lower", lambda c: f_leg_lower(c)), "lunge": ("lunge", lambda c: f_lunge(c)),
    "move": ("mountain", Yg("mountain")), "olympic_lift": ("clean", lambda c: f_olympic(c)), "plank": ("plank", lambda c: [plank_fore()]),
    "plyo": ("box_jump", f_box_jump), "pose": ("mountain", Yg("mountain")), "pull_up": ("pull_up", lambda c: f_pull_up(c)),
    "push_up": ("push_up", lambda c: f_push_up(c)), "row": ("row_bent", f_row_bent), "run": ("run", lambda c: f_run(c)),
    "run_indoor": ("run", lambda c: f_run(c)), "sandbag": ("squat", lambda c: f_squat(c)), "shoulder_press": ("press", lambda c: f_press(c)),
    "shoulder_stability": ("ext_rotation", f_ext_rotation), "shrug": ("shrug", f_shrug), "sit_up": ("sit_up", lambda c: f_situp(c)),
    "sled": ("sled", lambda c: f_sled(c)), "sledge_hammer": ("hammer_slam", lambda c: f_slam(c, "hammer")), "squat": ("squat", lambda c: f_squat(c)),
    "stair_stepper": ("stepper", f_stepper), "suspension": ("inverted_row", f_inverted_row), "tire": ("tire_flip", f_tire_flip),
    "total_body": ("burpee", lambda c: f_burpee(c)), "triceps_extension": ("triceps_overhead", f_triceps_overhead),
    "warm_up": ("march", lambda c: f_high_knees(c, "march")),
}

# Category-specific rules, checked before RULES.
CATEGORY_RULES = {
    "run": [(r"walk|warm_up", lambda c: f_run(c, 0.5, 2), "walk"), (r"sprint", lambda c: f_run(c, 1.3, 12), "sprint"), (r"jog", lambda c: f_run(c, 0.8), "jog"),
            (r"", lambda c: f_run(c), "run")],
    "run_indoor": [(r"", lambda c: f_run(c), "run")],
    "ladder": [(r"", lambda c: f_run(c, 0.8, knee_up=20), "agility")],
    "bike": [(r"", f_bike, "bike")], "bike_outdoor": [(r"", f_bike, "bike")], "indoor_bike": [(r"", f_bike, "bike")],
    "elliptical": [(r"", f_stepper, "stepper")], "stair_stepper": [(r"", f_stepper, "stepper")], "floor_climb": [(r"", lambda c: f_stepper(c, False), "stepper")],
    "indoor_row": [(r"", f_rower, "rower")], "tire": [(r"", f_tire_flip, "tire_flip")],
    "sled": [(r"backward", lambda c: f_sled(c, "drag_back"), "sled_drag_back"), (r"forward_drag", lambda c: f_sled(c, "drag"), "sled_drag"),
             (r"chest_press", f_chest_press_standing, "chest_press_standing"), (r"row", lambda c: f_row_seated(c, True), "row_standing"),
             (r"", lambda c: f_sled(c, "push"), "sled_push")],
    "sledge_hammer": [(r"lateral", lambda c: f_slam(c, "side_throw"), "side_throw"), (r"", lambda c: f_slam(c, "hammer"), "hammer_slam")],
    "battle_rope": [(r"jumping_jacks", lambda c: f_jumping_jack(c), "jumping_jacks"), (r"russian_twist", f_russian_twist, "russian_twist"),
                    (r"(slam|stage_coach)", lambda c: f_battle_rope(c, "slam"), "rope_slam"), (r"(double_arm_wave|in_and_out|clap)", lambda c: f_battle_rope(c, "double"), "rope_double"),
                    (r"(side_to_side|side_wave|snake|sidewinder|hip_toss|grappler)", lambda c: f_battle_rope(c, "side"), "rope_side"),
                    (r"(figure_eight|circle)", lambda c: f_battle_rope(c, "circle"), "rope_circle"), (r"lunge", lambda c: f_battle_rope(c, "lunge"), "rope_lunge"),
                    (r"kneeling", lambda c: f_battle_rope(c, "kneel"), "rope_kneel"), (r"side_plank", lambda c: f_side_plank(c), "side_plank"),
                    (r"upper_cuts", lambda c: f_battle_rope(c, "uppercut"), "rope_uppercut"), (r"rainbow", lambda c: f_battle_rope(c, "rainbow"), "rope_rainbow"),
                    (r"split_jack", lambda c: f_jumping_jack(c, "split"), "split_jacks"), (r"jump", lambda c: f_box_jump(c), "box_jump"),
                    (r"", lambda c: f_battle_rope(c), "rope_wave")],
    "suspension": [(r"chest_fly", lambda c: f_flye(c, True), "flye_standing"), (r"chest_press", f_chest_press_standing, "chest_press_standing"),
                   (r"^crunch", lambda c: f_reverse_crunch(c) if False else f_mountain_climber(c), "mountain_climber"), (r"curl", f_curl, "curl"),
                   (r"glute_bridge|hip_drop", lambda c: f_bridge(c), "bridge"), (r"hamstring_curl", f_leg_curl, "leg_curl"),
                   (r"knee_drive_jump", lambda c: f_high_knees(c), "high_knees"), (r"knee_to_chest|^mountain_climber|reverse_mountain", f_mountain_climber, "mountain_climber"),
                   (r"lat_pullover", lambda c: f_pulldown(c, True), "straight_arm_pulldown"), (r"pendulum", lambda c: f_side_plank(c), "side_plank"),
                   (r"pike", f_jackknife, "jackknife"), (r"^plank$", lambda c: [plank_high()], "plank_high"), (r"power_pull", f_inverted_row, "inverted_row"),
                   (r"reverse_plank", f_reverse_plank, "reverse_plank"), (r"rollout", lambda c: f_rollout(c, False), "rollout_standing"),
                   (r"single_leg_deadlift", lambda c: f_deadlift(c, "single"), "single_leg_deadlift"), (r"split", lambda c: f_lunge(c, "bulgarian"), "bulgarian"),
                   (r"tricep_press", f_triceps_overhead, "triceps_overhead"), (r"y_fly", lambda c: f_raise(c, "v"), "v_raise"), (r"sit_up", lambda c: f_situp(c), "sit_up")],
    "move": [(r"pilates_plie", lambda c: f_squat(c, wide=True), "squat"), (r"lunges_with_knee_lift", lambda c: f_lunge(c), "lunge"),
             (r"hamstring_curl", f_leg_curl, "leg_curl"), (r"straight_leg_crunch|v_leg_crunch|^v_sit", None, None)],
    "pose": [],
}


def classify(cat, name):
    """(builder, label, quality) for an exercise; quality is family or category."""
    c = C(cat, name)
    name = re.sub(r"^weighted_|_with_weights$|_with_weight$", "", name)
    base = re.sub(r"_wheelchair$", "", name)
    if cat == "pose" and yoga(base) and "wheelchair" not in name:
        return (lambda c: yoga(base)()), "pose_" + base, "family", c
    for pat, fn, label in CATEGORY_RULES.get(cat, []):
        if fn is None:
            break
        if re.search(pat, name):
            return fn, label, "family", c
    if cat in ("warm_up", "move", "banded_exercises", "pose") or "stretch" in name:
        for pat, key in STRETCH_RULES:
            if re.search(pat, name):
                if key is None:
                    return (lambda c: yoga("supine_spinal_twist")()), "pose_supine_spinal_twist", "family", c
                return St(key), "stretch_" + key, "family", c
    for rcat, pat, fn, label in RULES:
        if rcat and rcat != cat:
            continue
        if re.search(pat, name):
            if label == "curl" and re.search(r"leg|hamstring|tailbone|arch_and|dead_curl|reverse_curl_and", name):
                continue
            return fn, label, "family", c
    if cat == "pose" or "stretch" in name:
        y = yoga(base)
        if y:
            return (lambda c: y()), "pose_" + base, "family", c
    label, fn = CATEGORY_DEFAULT.get(cat, ("mountain", Yg("mountain")))
    return fn, label, "category", c
