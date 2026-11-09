# Exercise figures: the Blender pipeline

Poses every exercise of Garmin's FIT profile on a see-through mannequin and writes what the app
ships: `web/src/figures/assets/mannequin.glb`, `internal/figures/data/catalog.json` and
`internal/figures/data/poses.json`. The whole story — the data, the pose language, the families
and their rules, and how to work on a pose — is in [docs/figures.md](../../docs/figures.md).

## Quick start

- **Rebuild everything:** `make figures`. It needs Blender 5.2 LTS (`BLENDER=…` if it is not
  `blender`).
- **Work on poses in Blender:** in the Scripting tab, open `blender/session.py` and run it, then
  use `fig.show("core/51")`, `fig.play(…)`, `fig.review(…)`, `fig.sheet(category=…)`,
  `fig.reload()` and `fig.export()` in the Python console.
- **Contact sheets from a folder of renders:** `python3 tools/figures/sheets.py DIR`
  (ImageMagick).

## What is here

| Path | What |
|---|---|
| `exercises/` | Go: prints the profile's exercises (the work list) |
| `blender/mannequin.blend` | the body: Blender Studio's *Human Base Meshes* v1.4.1, CC0 1.0 (only the athletic male primitive body; `make figures-asset BUNDLE=…` writes it again from the bundle) |
| `blender/rig.py` | loads the body, aims its parts, floors a pose, levels it on its contacts |
| `blender/fk.py` | the pose language: joint angles, world aims, contacts |
| `blender/own.py` | the exercises posed one by one |
| `blender/families.py` | the movement families and the rules that give every exercise one |
| `blender/catalog.py`, `blender/export.py`, `blender/slim_glb.py` | the catalogue and the files the app ships |
| `blender/build.py` | the headless entry `make figures` runs |
| `blender/session.py` | the interactive entry for a running Blender |
| `blender/review.py`, `sheets.py` | review renders and contact sheets |
| `blender/extract.py` | writes `mannequin.blend` from the bundle |
