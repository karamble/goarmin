// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/** Where goarmin's source lives (the About dialog links to it). */
export const SOURCE_URL = 'https://github.com/karamble/goarmin'

/** Under every exercise figure, and in the disclaimer. */
export const FIGURE_DISCLAIMER =
  'This visualization is for illustrative purposes only; accuracy is not guaranteed. Please consult a professional instructor.'

/**
 * The disclaimer of liability, shown in About. The README's Disclaimer section says the same;
 * change both together.
 */
export const DISCLAIMER: { lead: string; text: string }[] = [
  {
    lead: 'Use at your own risk.',
    text: 'goarmin is provided “as is”, without warranty of any kind, express or implied. The authors and contributors accept no liability for any damage or loss arising from its use or from the inability to use it, including inaccurate, incomplete or lost data.',
  },
  {
    lead: 'No medical advice.',
    text: 'goarmin is not a medical device. It shows what your devices recorded, as it reads their files; the accuracy and completeness of the data and of the values derived from it are not guaranteed. Do not base medical, health or training decisions on it; consult a physician.',
  },
  {
    lead: 'Exercise figures.',
    text: `The exercise figures are simplified visualizations of each movement, without equipment. ${FIGURE_DISCLAIMER} Exercise at your own risk.`,
  },
]
