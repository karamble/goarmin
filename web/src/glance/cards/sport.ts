// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Activity, Bike, Dumbbell, Footprints, Mountain, PersonStanding, Waves, type LucideIcon } from 'lucide-react'

/** sportIcon picks an icon for a FIT sport name ("running", "cycling", …). */
export function sportIcon(sport: string): LucideIcon {
  switch (sport) {
    case 'running':
      return Footprints
    case 'cycling':
    case 'e_biking':
      return Bike
    case 'walking':
      return PersonStanding
    case 'hiking':
    case 'mountaineering':
      return Mountain
    case 'swimming':
      return Waves
    case 'training':
    case 'fitness_equipment':
      return Dumbbell
    default:
      return Activity
  }
}
