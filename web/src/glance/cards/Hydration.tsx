// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { dailyValueCard } from './DailyValue'

/** Hydration: what you logged drinking. */
export default dailyValueCard({ key: 'hydration', unit: 'ml', digits: 0, label: 'logged', empty: 'No drinks logged' })
