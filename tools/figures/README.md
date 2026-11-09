# Exercise figures

The dashboard shows each exercise of a workout as a see-through 3D mannequin (Training ›
Exercises, and *View pose* on a workout's steps). This folder builds what it draws:

- `web/src/figures/assets/mannequin.glb`: the mannequin in its rest pose, mesh only;
- `internal/figures/data/catalog.json`: every exercise of Garmin's FIT profile (1,846 in 51
  categories) with the pose set it uses;
- `internal/figures/data/poses.json`: the key poses: the root's position and one local rotation
  per moving part, in glTF axes.

Rebuild them with `make figures`. That needs Blender (5.2 LTS, `BLENDER=…`) and Blender Studio's
**Human Base Meshes** bundle v1.4.1 (CC0), whose `.blend` `FIGURES_BUNDLE` points to. Only the
generated files are in the repository; the bundle is not.

## How the poses are made

- `exercises/` (Go) lists the profile's exercises.
- `blender/rig.py` loads the athletic male primitive body (its parts already hinge at the joints)
  and aims parts: a part's axis towards its child joint points along a direction, its front along
  another. A pose is floored, and with `contacts` the body tilts until two supports (hands and
  feet, forearms and feet, …) both touch the floor.
- `blender/fk.py` turns joint angles (trunk flexion, side bend and twist; shoulder, elbow, hip,
  knee and ankle) and world aims for supporting limbs into such a pose.
- `blender/own.py` poses the exercises of the author's own workouts one by one.
- `blender/families.py` holds about 170 movement families (squat, lunge, row, plank, yoga poses,
  stretches, …) and the rules that map every exercise name onto one; alternating exercises are
  mirrored.
- `blender/catalog.py` and `blender/export.py` build the catalogue and write the files;
  `blender/build.py` is the headless entry point `make figures` runs.
- `blender/review.py` renders review images when poses change (`render_ex`, `shoot_quick`).

A hold has one key pose; a movement loops between two to five. The dashboard blends them (slerp)
and holds the first key when the system asks for reduced motion.
