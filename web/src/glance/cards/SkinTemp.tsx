// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { dailyValueCard } from './DailyValue'

/** SkinTemp: last night's skin temperature against your baseline. */
export default dailyValueCard({ key: 'skin_temp_deviation', unit: '°C', digits: 1, signed: true, label: 'from your baseline', empty: 'No skin temperature yet' })
