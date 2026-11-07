// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { ChartTokens } from '../theme/tokens'

/** Stage codes (FIT sleep_level). */
export const STAGE = { awake: 1, light: 2, deep: 3, rem: 4 } as const

/** stageColor: deep cyan, light lime-dim, REM pink, awake amber. */
export function stageColor(t: ChartTokens, stage: number): string {
  switch (stage) {
    case STAGE.deep:
      return t.cyan
    case STAGE.light:
      return t.limeDim
    case STAGE.rem:
      return t.pink
    default:
      return t.amber
  }
}
