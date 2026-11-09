// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useEffect, useRef } from 'react'
import * as THREE from 'three'
import { GLTFLoader, type GLTF } from 'three/addons/loaders/GLTFLoader.js'
import { OrbitControls } from 'three/addons/controls/OrbitControls.js'
import mannequinUrl from './assets/mannequin.glb?url'
import type { ExercisePose } from './api'
import { blendAt } from './motion'

// The look decided for the figures: X-ray (additive), fill 0.6, rim 3.1, rim sharpness 2.0, the
// left and right side in two lime tones, on the card colour.
const LOOK = { fill: 0.6, rim: 3.1, sharpness: 2.0 }
const TONES = { R: '#c8f542', L: '#8da92e', C: '#aecd3a' }
const CARD = '#161b22'

export type View = 'three' | 'front' | 'side' | 'back' | 'top'
const VIEWS: Record<View, [number, number, number]> = {
  three: [0.75, 0.45, 0.9], front: [0, 0.15, 1], side: [1, 0.15, 0], back: [0, 0.15, -1], top: [0.001, 1, 0.001],
}

const vertexShader = /* glsl */ `
  varying vec3 vN; varying vec3 vV;
  void main() {
    vec4 mv = modelViewMatrix * vec4(position, 1.0);
    vN = normalize(normalMatrix * normal);
    vV = normalize(-mv.xyz);
    gl_Position = projectionMatrix * mv;
  }`
const fragmentShader = /* glsl */ `
  uniform vec3 uColor; uniform float uFill, uRim, uPow;
  varying vec3 vN; varying vec3 vV;
  void main() {
    vec3 n = normalize(vN);
    if (!gl_FrontFacing) n = -n;
    float rim = pow(clamp(1.0 - abs(dot(n, normalize(vV))), 0.0, 1.0), uPow);
    gl_FragColor = vec4(uColor * (uFill * 0.35 + rim * uRim * 0.7), clamp(uFill * 0.5 + rim, 0.0, 1.0));
    #include <colorspace_fragment>
  }`

function material(tone: string) {
  return new THREE.ShaderMaterial({
    vertexShader, fragmentShader, transparent: true, depthWrite: false, side: THREE.DoubleSide, blending: THREE.AdditiveBlending,
    uniforms: { uColor: { value: new THREE.Color(tone) }, uFill: { value: LOOK.fill }, uRim: { value: LOOK.rim }, uPow: { value: LOOK.sharpness } },
  })
}

// One download of the mannequin for the page's life; each canvas draws a clone (shared geometry).
let mannequin: Promise<GLTF> | null = null
function loadMannequin() {
  mannequin ??= new GLTFLoader().loadAsync(mannequinUrl)
  return mannequin
}

/**
 * FigureCanvas draws the mannequin in an exercise's key poses: a hold stays still, a loop moves
 * between its keys. Drag to turn it; view picks the camera. With reduced motion it shows the key
 * pose keyIndex and does not move. It pauses off screen and in a hidden tab, and gives its WebGL
 * context back when it unmounts.
 */
export default function FigureCanvas({ pose, view, reduced, keyIndex = 0, label }: {
  pose: ExercisePose
  view: View
  reduced: boolean
  keyIndex?: number
  label: string
}) {
  const ref = useRef<HTMLCanvasElement>(null)
  const state = useRef({ pose, view, reduced, keyIndex })
  state.current = { pose, view, reduced, keyIndex }
  const api = useRef<{ frame: () => void } | null>(null)

  useEffect(() => {
    const canvas = ref.current!
    const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: false })
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
    renderer.setClearColor(CARD)
    const scene = new THREE.Scene()
    const camera = new THREE.PerspectiveCamera(35, 4 / 3, 0.05, 50)
    const controls = new OrbitControls(camera, canvas)
    controls.enableDamping = true
    controls.enablePan = false
    const floor = new THREE.Mesh(new THREE.CircleGeometry(1.6, 64), new THREE.MeshBasicMaterial({ color: '#1f262e' }))
    floor.rotation.x = -Math.PI / 2
    scene.add(floor)
    const materials = { R: material(TONES.R), L: material(TONES.L), C: material(TONES.C) }
    const nodes: Record<string, THREE.Object3D> = {}
    let figure: THREE.Object3D | null = null
    let alive = true, visible = true, raf = 0, clock = 0, last = performance.now()
    const qa = new THREE.Quaternion(), qb = new THREE.Quaternion()

    const apply = (a: number[], b: number[], u: number) => {
      const { parts } = state.current.pose
      nodes.pelvis?.position.set(a[0] + (b[0] - a[0]) * u, a[1] + (b[1] - a[1]) * u, a[2] + (b[2] - a[2]) * u)
      parts.forEach((name, i) => {
        const o = 3 + 4 * i
        qa.set(a[o], a[o + 1], a[o + 2], a[o + 3])
        qb.set(b[o], b[o + 1], b[o + 2], b[o + 3])
        nodes[name]?.quaternion.slerpQuaternions(qa, qb, u)
      })
    }
    const frame = () => { // fit the camera around every key pose, from the chosen view
      if (!figure) return
      const { keys } = state.current.pose
      const box = new THREE.Box3()
      for (const k of keys) {
        apply(k, k, 0)
        figure.updateMatrixWorld(true)
        box.union(new THREE.Box3().setFromObject(figure, true))
      }
      const c = box.getCenter(new THREE.Vector3())
      const size = box.getSize(new THREE.Vector3()).length() || 2
      const d = new THREE.Vector3(...VIEWS[state.current.view]).normalize()
      camera.position.copy(c).addScaledVector(d, size * 1.7)
      controls.target.copy(c)
      camera.near = size / 50
      camera.far = size * 20
      camera.updateProjectionMatrix()
      controls.update()
      clock = 0
    }
    api.current = { frame }

    const resize = () => {
      const w = canvas.clientWidth, h = canvas.clientHeight
      if (!w || !h) return
      renderer.setSize(w, h, false)
      camera.aspect = w / h
      camera.updateProjectionMatrix()
    }
    const tick = (now: number) => {
      raf = 0
      if (!alive) return
      const dt = Math.min(0.1, (now - last) / 1000)
      last = now
      if (figure) {
        const { keys } = state.current.pose
        if (state.current.reduced) {
          const k = keys[Math.min(state.current.keyIndex, keys.length - 1)]
          apply(k, k, 0)
        } else {
          clock += dt
          const { a, b, u } = blendAt(keys.length, clock)
          apply(keys[a], keys[b], u)
        }
      }
      controls.update()
      renderer.render(scene, camera)
      if (visible && !document.hidden) raf = requestAnimationFrame(tick)
    }
    const wake = () => {
      if (alive && !raf && visible && !document.hidden) {
        last = performance.now()
        raf = requestAnimationFrame(tick)
      }
    }

    const ro = new ResizeObserver(() => { resize(); wake() })
    ro.observe(canvas)
    const io = new IntersectionObserver(([e]) => { visible = e.isIntersecting; wake() })
    io.observe(canvas)
    document.addEventListener('visibilitychange', wake)

    loadMannequin().then((gltf) => {
      if (!alive) return
      figure = gltf.scene.clone(true)
      figure.traverse((o) => {
        const part = o.userData.goarmin_part as string | undefined
        if (part) nodes[part] = o
        if ((o as THREE.Mesh).isMesh) {
          const side = part?.endsWith('.L') ? 'L' : part?.endsWith('.R') ? 'R' : 'C'
          ;(o as THREE.Mesh).material = materials[side]
        }
      })
      scene.add(figure)
      resize()
      frame()
      wake()
    })

    return () => {
      alive = false
      if (raf) cancelAnimationFrame(raf)
      ro.disconnect()
      io.disconnect()
      document.removeEventListener('visibilitychange', wake)
      controls.dispose()
      Object.values(materials).forEach((m) => m.dispose())
      floor.geometry.dispose()
      ;(floor.material as THREE.Material).dispose()
      renderer.dispose()
      renderer.forceContextLoss()
      api.current = null
    }
  }, [])

  // A new exercise or view refits the camera; a key step under reduced motion just redraws.
  useEffect(() => { api.current?.frame() }, [pose, view])

  return <canvas ref={ref} role="img" aria-label={label} className="block aspect-[4/3] w-full touch-none rounded-xl" />
}
