# Copyright (c) 2026 karamble
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
# goarmin figures: the user's 45 exercises (from the watch's workouts), each with its own pose.
# key "category/number": (title, keys). One key is a hold; several loop in order.
# Helpers build the common bases; poses come from what each exercise is (looked at, never copied).


# World aims for supporting limbs (prone and supine: the head is towards F, the feet towards B).
SUPPORT_ARM = {"arm_upper": (D, F), "arm_lower": (D, F), "hand": (F, "medial")}  # straight, under the shoulder
FOREARM = {"arm_upper": (D, F), "arm_lower": (F, "medial"), "hand": (F, "medial")}  # forearm flat, towards the head
KNEEL = {"leg_upper": (D, F), "leg_lower": (B, D), "foot": (B, D)}  # thigh down, shin and foot flat
TOES = {"foot": (D, F)}  # prone on the toes
SHIN_HOLD = {"arm_upper": (ang(B, U, 35), U), "arm_lower": (ang(B, U, 15), U), "hand": (ang(B, D, 15), "medial")}


def reach_arm(s):
    """A straight arm reaching towards the head's side (prone: forwards along the floor's level)."""
    return {f"arm_upper.{s}": (F, U), f"arm_lower.{s}": (F, U), f"hand.{s}": (F, "medial")}


def reach_leg(s):
    """A straight leg reaching back at hip height."""
    return {f"leg_upper.{s}": (B, D), f"leg_lower.{s}": (B, D), f"foot.{s}": (B, D)}


def quad(aim=None, **kw):
    """All fours: arms straight under the shoulders, knees under the hips, tops of the feet down."""
    return P("prone", aim={**SUPPORT_ARM, **KNEEL, **(aim or {})}, contacts=("hands", "knees"), **kw)


def plank_fore(aim=None, **kw):
    return P("prone", aim={**FOREARM, **TOES, **(aim or {})}, contacts=("forearms", "feet"), **kw)


def plank_high(aim=None, contacts=("hands", "feet"), **kw):
    return P("prone", aim={**SUPPORT_ARM, **TOES, **(aim or {})}, contacts=contacts, **kw)


HANDS_BEHIND_HEAD = {"flex": 160, "abd": 55, "elbow": 140}


def behind_head_aim():
    """Supine curl, hands behind the head, elbows wide (world aims)."""
    out = {}
    for sd, lat in (("L", LT), ("R", RT)):
        out[f"arm_upper.{sd}"] = ((lat * 0.85 + F * 0.45 + U * 0.3).normalized(), U)
        out[f"arm_lower.{sd}"] = ((-lat * 0.9 + D * 0.35 + F * 0.15).normalized(), U)
        out[f"hand.{sd}"] = ((-lat * 0.6 + D * 0.5).normalized(), U)
    return out


def supine_curl(trunk=35, **kw):
    """On the back, head and shoulders lifted (the pilates curl)."""
    return P("supine", spine=(trunk, 0, 0), neck=(12, 0, 0), **kw)


def seated(**kw):
    """Sitting upright, legs long in front (they stay level when the pelvis tilts forward)."""
    legs = {"flex": 90 + kw.get("pelvis", (0, 0, 0))[0]}
    legs.update(kw.pop("legs", {}))
    return P(legs=legs, **kw)


CROSS_LEGS = {"LL": {"flex": 80, "abd": 40, "rot": 70, "knee": 150, "ankle": 10},
              "RL": {"flex": 80, "abd": 40, "rot": 70, "knee": 150, "ankle": 10}}


def low_lunge(front="L", back_knee=True, **kw):
    """Front foot flat with the knee over the ankle, back knee down (back_knee) or the back leg long."""
    f = {"flex": 95, "knee": 95, "ankle": 0}
    b = {"flex": -25, "knee": 95, "ankle": 85} if back_knee else {"flex": -55, "knee": 0, "ankle": -15}
    back = "R" if front == "L" else "L"
    legs = {"LL": f, "RL": b} if front == "L" else {"LL": b, "RL": f}
    for k in ("LL", "RL"):  # overrides merge into the lunge's legs
        legs[k] = {**legs[k], **kw.pop(k, {})}
    kw.setdefault("contacts", (f"feet.{front}", f"knees.{back}" if back_knee else f"feet.{back}", "x", 20))
    return P(LL=legs["LL"], RL=legs["RL"], **kw)


def down_dog(**kw):
    """Hips high, arms in line with the trunk, legs long and inclined, heels towards the floor."""
    aim = {"leg_upper": (ang(D, B, 30), F), "leg_lower": (ang(D, B, 30), F), "foot": (ang(F, D, 25), U),
           "hand": (F, "medial"), **kw.pop("aim", {})}
    return P(pelvis=(135, 0, 0), arms={"flex": 180}, aim=aim, contacts=("hands", "feet"), **kw)


def bridge(height=1.0, **kw):
    """On the back, knees bent, feet flat, hips lifted (height 0 lowered, 1 high)."""
    h = height
    aim = {"leg_upper": (ang(B, U, 45 - 30 * h), U), "leg_lower": (ang(D, F, 20 * h - 10), B), "foot": (B, U),
           "arm_upper": (B, U), "arm_lower": (B, U), "hand": (B, "medial")}
    return P("supine", pelvis=(-40 * h, 0, 0), aim=aim, contacts=("feet", "back"), **kw)


USER = {
    # --- core (pilates) ---
    "core/48": ("Arm and Leg Extension on Knees", [
        quad(),
        quad(aim={**reach_arm("L"), **reach_leg("R")}),
        quad(),
        quad(aim={**reach_arm("R"), **reach_leg("L")})]),
    "core/51": ("Cat Cow", [
        quad(pelvis=(-12, 0, 0), spine=(28, 0, 0), neck=(25, 0, 0), head=(20, 0, 0)),
        quad(pelvis=(10, 0, 0), spine=(-25, 0, 0), neck=(-25, 0, 0), head=(-15, 0, 0))]),
    "core/53": ("Criss Cross", [
        P("supine", spine=(35, 0, 30), neck=(12, 0, 0), aim=behind_head_aim(),
          LL={"flex": 110, "knee": 110}, RL={"flex": 40, "ankle": 30}),
        P("supine", spine=(35, 0, -30), neck=(12, 0, 0), aim=behind_head_aim(),
          RL={"flex": 110, "knee": 110}, LL={"flex": 40, "ankle": 30})]),
    "core/55": ("Double-leg Stretch", [
        supine_curl(legs={"flex": 120, "knee": 120, "ankle": 30}, aim=SHIN_HOLD),
        supine_curl(arms={"flex": 170}, legs={"flex": 45, "ankle": 30})]),
    "core/60": ("Roll Over", [
        P("supine", arms={"abd": 15}, legs={"flex": 90, "ankle": 30}),
        P("supine", pelvis=(-75, 0, 0), spine=(40, 0, 0),
          aim={"leg_upper": (ang(F, D, 15), D), "leg_lower": (ang(F, D, 15), D), "foot": (ang(F, D, 15), D),
               "arm_upper": (B, U), "arm_lower": (B, U), "hand": (B, "medial")})]),
    "core/61": ("Roll Up", [
        P("supine", arms={"flex": 170}, legs={"ankle": -15}),
        seated(pelvis=(5, 0, 0), spine=(55, 0, 0), neck=(15, 0, 0), legs={"flex": 95, "ankle": -15},
               aim={"arm_upper": (ang(F, D, 10), U), "arm_lower": (ang(F, D, 10), U), "hand": (F, "medial")})]),
    "core/67": ("Single-leg Stretch", [
        supine_curl(LL={"flex": 120, "knee": 120, "ankle": 30}, RL={"flex": 45, "ankle": 30}, aim=SHIN_HOLD),
        supine_curl(RL={"flex": 120, "knee": 120, "ankle": 30}, LL={"flex": 45, "ankle": 30}, aim=SHIN_HOLD)]),
    "core/69": ("Swan", [
        P("prone", arms={"flex": 150, "elbow": 110}, legs={"ankle": 85}, hands=(F, "medial")),
        P("prone", spine=(-50, 0, 0), neck=(-15, 0, 0), arms={"flex": 60, "elbow": 5}, legs={"ankle": 85}, hands=(F, "medial"))]),
    "core/70": ("Swimming", [
        P("prone", spine=(-15, 0, 0), neck=(-10, 0, 0), L={"flex": 175}, R={"flex": 150},
          LL={"flex": -5, "ankle": 80}, RL={"flex": -20, "ankle": 80}),
        P("prone", spine=(-15, 0, 0), neck=(-10, 0, 0), R={"flex": 175}, L={"flex": 150},
          RL={"flex": -5, "ankle": 80}, LL={"flex": -20, "ankle": 80})]),
    "core/72": ("The Hundred", [
        supine_curl(arms={"flex": 25}, legs={"flex": 45, "ankle": 30})]),
    # --- back ---
    "hyperextension/38": ("Cobra", [
        P("prone", spine=(-55, 0, 0), neck=(-10, 0, 0), arms={"flex": 20, "elbow": 30}, legs={"ankle": 90}, hands=(F, "medial"))]),
    # --- shoulders ---
    "lateral_raise/32": ("Arm Circles", [
        P(arms={"abd": 90, "flex": 12}), P(arms={"abd": 102}), P(arms={"abd": 90, "flex": -12}), P(arms={"abd": 78})]),
    # --- lunges ---
    "lunge/30": ("Low Lunge with Isometric Adduction", [
        low_lunge("L", L={"flex": 40, "elbow": 60, "abd": 10}, R={"flex": 40, "elbow": 60, "abd": 10})]),
    "lunge/34": ("Lunge with Arm Reach", [
        P(arms={"abd": 8}),
        low_lunge("L", back_knee=False, arms={"flex": 175})]),
    # --- moves (mobility) ---
    "move/2": ("Arm Stretch", [
        P(R={"flex": 90, "abd": -60, "pron": 0}, L={"flex": 70, "abd": -30, "elbow": 110})]),
    "move/31": ("Piriformis Stretch", [
        P("supine", aim={
            "leg_upper.L": (ang(U, F, 20), F), "leg_lower.L": (B, U), "foot.L": (U, B),
            "leg_upper.R": ((U + RT * 0.9 + F * 0.3).normalized(), F), "leg_lower.R": (LT, F),
            "foot.R": (ang(U, F, 30), LT),
            "arm_upper": (ang(U, B, 30), B), "arm_lower": (ang(U, B, 5), B), "hand": (ang(U, B, 5), "medial")})]),
    "move/34": ("Plank Up/Downs", [
        plank_fore(),
        plank_high(aim={"arm_lower.R": (F, "medial"), "hand.R": (F, "medial")},
                   contacts=[("forearms", "feet"), ("hands.L", "forearms.R", "y")]),
        plank_high(), plank_fore()]),
    "move/38": ("Roll Down", [
        P(arms={"abd": 6}),
        P(pelvis=(75, 0, 0), spine=(45, 0, 0), neck=(20, 0, 0), legs={"flex": 75}, arms={"flex": 110})]),
    "move/48": ("Spine Stretch Forward", [
        seated(arms={"flex": 90}, legs={"abd": 25, "flex": 90, "ankle": -20}),
        seated(spine=(60, 0, 0), neck=(20, 0, 0), arms={"flex": 80}, legs={"abd": 25, "flex": 90, "ankle": -20})]),
    # --- planks ---
    "plank/43": ("Plank", [plank_fore()]),
    "plank/54": ("Plank with Arm Raise", [
        plank_high(), plank_high(aim=reach_arm("L")), plank_high(), plank_high(aim=reach_arm("R"))]),
    "plank/58": ("Plank with Oblique Crunch", [
        plank_high(), plank_high(spine=(10, 0, 15), LL={"flex": 100, "knee": 110, "abd": -10},
                                 aim={"foot.L": (B, D)}),
        plank_high(), plank_high(spine=(10, 0, -15), RL={"flex": 100, "knee": 110, "abd": -10},
                                 aim={"foot.R": (B, D)})]),
    "plank/66": ("Side Plank", [
        P("side_left", aim={"arm_upper.L": (D, RT), "arm_lower.L": (RT, F), "hand.L": (RT, F),
                            "arm_upper.R": (U, RT), "arm_lower.R": (U, RT), "hand.R": (U, F), "foot": (RT, F)},
          contacts=("forearms.L", "feet"))]),
    "plank/130": ("Bridge with Glute Lower Lift", [bridge(0.3), bridge(1.0)]),
    "plank/132": ("Plank (with Arm Variations)", [
        plank_high(), plank_high(aim={"arm_upper.L": (LT, F), "arm_lower.L": (LT, F), "hand.L": (LT, D)}),
        plank_high(), plank_high(aim={"arm_upper.R": (RT, F), "arm_lower.R": (RT, F), "hand.R": (RT, D)})]),
    "plank/133": ("Plank with Leg Lift", [
        plank_fore(), plank_fore(LL={"flex": -25}, aim={"foot.L": (ang(B, U, 25), D)}),
        plank_fore(), plank_fore(RL={"flex": -25}, aim={"foot.R": (ang(B, U, 25), D)})]),
    # --- yoga poses ---
    "pose/8": ("Bridge Pose", [bridge(1.0)]),
    "pose/11": ("Child's Pose", [
        P(aim={"pelvis": (ang(F, D, 25), D), "belly": (ang(F, D, 25), D), "chest": (ang(F, D, 25), D),
               "neck": (ang(F, D, 45), D), "head": (ang(F, D, 65), D),
               "arm_upper": (ang(F, D, 12), U), "arm_lower": (ang(F, D, 6), "medial"), "hand": (F, "medial"),
               "leg_upper": (ang(F, D, 33), U), "leg_lower": (B, D), "foot": (B, D)})]),
    "pose/17": ("Dolphin Pose", [
        P(pelvis=(140, 0, 0), aim={"arm_upper": (ang(D, F, 15), F), "arm_lower": (F, "medial"), "hand": (F, "medial"),
                                   "leg_upper": (ang(D, B, 25), F), "leg_lower": (ang(D, B, 25), F),
                                   "foot": (ang(F, D, 25), U)}, contacts=("forearms", "feet"))]),
    "pose/21": ("Downward Facing Dog Pose", [down_dog()]),
    "pose/24": ("Extended Puppy Pose", [
        P(pelvis=(90, 0, 0), spine=(-15, 0, 0), neck=(10, 0, 0), arms={"flex": 175},
          legs={"flex": 90, "knee": 90, "ankle": 90}, hands=(F, "medial"))]),
    "pose/25": ("Extended Side Angle Pose", [
        P(aim={"pelvis": (ang(U, LT, 50), F), "belly": (ang(U, LT, 55), F), "chest": (ang(U, LT, 60), F),
               "neck": (ang(U, LT, 60), F), "head": (ang(U, LT, 45), F),
               "leg_upper.L": (ang(LT, D, 10), U), "leg_lower.L": (D, LT), "foot.L": (LT, U),
               "leg_upper.R": (ang(D, RT, 40), F), "leg_lower.R": (ang(D, RT, 40), F), "foot.R": (ang(F, RT, 30), U),
               "arm_upper.L": (D, F), "arm_lower.L": (D, F), "hand.L": (D, LT),
               "arm_upper.R": (ang(U, LT, 60), F), "arm_lower.R": (ang(U, LT, 60), F), "hand.R": (ang(U, LT, 60), F)},
          contacts=("feet.L", "feet.R", "y"))]),
    "pose/37": ("Lizard Pose", [
        low_lunge("L", back_knee=False, pelvis=(70, 0, 0), spine=(10, 0, 0), neck=(-20, 0, 0),
                  aim={**SUPPORT_ARM, "leg_upper.L": (ang(F, D, 15), U), "leg_lower.L": (ang(D, B, 5), F),
                       "foot.L": (F, U), "leg_upper.R": (ang(B, D, 23), D), "leg_lower.R": (ang(B, D, 23), D),
                       "foot.R": (D, B)},
                  contacts=("hands", "feet.R"))]),
    "pose/40": ("Low Lunge Twist Pose", [
        low_lunge("L", spine=(25, 0, 45), L={"flex": 40, "abd": 20, "elbow": 120}, R={"flex": 40, "abd": -20, "elbow": 120})]),
    "pose/54": ("Seated Easy Side Bend Pose", [
        seated(spine=(0, 30, 0), L={"abd": 40, "elbow": 10}, R={"abd": 170, "elbow": 10}, **CROSS_LEGS)]),
    "pose/55": ("Seated Easy Twist Pose", [
        seated(spine=(0, 0, 45), L={"flex": -30, "abd": 20}, R={"flex": 40, "abd": -25, "elbow": 40}, **CROSS_LEGS)]),
    "pose/56": ("Seated Long-Leg Forward Bend Pose", [
        seated(pelvis=(20, 0, 0), spine=(50, 0, 0), neck=(15, 0, 0), arms={"flex": 75}, legs={"flex": 110, "ankle": -15})]),
    "pose/62": ("Squat Open Arm Twist Pose", [
        P(pelvis=(30, 0, 0), spine=(25, 0, 50), L={"flex": 70, "abd": 10}, R={"abd": 150},
          legs={"flex": 110, "knee": 135, "ankle": -30, "abd": 25, "rot": 25})]),
    "pose/72": ("Standing Wide-Leg Forward Bend with Twist Pose", [
        P(pelvis=(85, 0, 0), spine=(10, 0, 45), legs={"flex": 85, "abd": 25},
          aim={"arm_upper.L": (D, F), "arm_lower.L": (D, F), "hand.L": (D, RT),
               "arm_upper.R": (U, F), "arm_lower.R": (U, F), "hand.R": (U, RT)})]),
    "pose/80": ("Up Dog Pose", [
        P("prone", spine=(-55, 0, 0), neck=(-15, 0, 0), legs={"flex": -12},
          aim={**SUPPORT_ARM, "foot": (B, D)}, contacts=("hands", "feet"))]),
    "pose/114": ("Downward Facing Dog to Cobra", [
        down_dog(),
        P("prone", spine=(-55, 0, 0), neck=(-10, 0, 0), arms={"flex": 20, "elbow": 30}, legs={"ankle": 90}, hands=(F, "medial"))]),
    # --- warm-up ---
    "warm_up/13": ("Neck Rotations", [
        P(arms={"abd": 6}, neck=(25, 0, 0), head=(10, 0, 0)), P(arms={"abd": 6}, neck=(0, 30, 0)),
        P(arms={"abd": 6}, neck=(-25, 0, 0)), P(arms={"abd": 6}, neck=(0, -30, 0))]),
    "warm_up/73": ("Three-Way Ankle Mobilization", [
        low_lunge("L", arms={"flex": 30, "elbow": 40}),
        low_lunge("L", arms={"flex": 30, "elbow": 40}, LL={"flex": 100, "knee": 120, "ankle": -25}),
        low_lunge("L", arms={"flex": 30, "elbow": 40}, LL={"flex": 100, "knee": 120, "ankle": -25, "abd": -12}),
        low_lunge("L", arms={"flex": 30, "elbow": 40}, LL={"flex": 100, "knee": 120, "ankle": -25, "abd": 12})]),
    "warm_up/74": ("90/90 Hip Switch with Reach", [
        seated(R={"flex": 170}, aim={"leg_upper.L": (F, U), "leg_lower.L": (RT, F), "foot.L": (RT, F),
                                     "leg_upper.R": (RT, F), "leg_lower.R": (B, RT), "foot.R": (B, RT)}),
        seated(L={"flex": 170}, aim={"leg_upper.R": (F, U), "leg_lower.R": (LT, F), "foot.R": (LT, F),
                                     "leg_upper.L": (LT, F), "leg_lower.L": (B, LT), "foot.L": (B, LT)})]),
    "warm_up/83": ("Chest Openers", [
        P(arms={"flex": 90}), P(arms={"abd": 90, "flex": -25})]),
}
