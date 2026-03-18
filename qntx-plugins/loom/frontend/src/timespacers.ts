// Discrete time spacers for temporal alignment across columns.
// One bar per 3h block at full size (24px), diminishing beyond 12h.

const SPACER_PX = 24
const SPACER_HOURS = 3
const SPACER_FULL_BLOCKS = 4 // 12h at full size
const SPACER_SHRINK = 2 // px reduction per block beyond 12h

export function timeSpacers(prevTs: number, curTs: number): number[] {
  if (prevTs === 0) return []
  const deltaMs = Math.max(0, curTs - prevTs)
  const hours = deltaMs / (60 * 60 * 1000)
  const blocks = Math.floor(hours / SPACER_HOURS)
  if (blocks === 0) return []
  const result: number[] = []
  for (let i = 0; i < blocks; i++) {
    if (i < SPACER_FULL_BLOCKS) {
      result.push(SPACER_PX)
    } else {
      const shrunk = SPACER_PX - (i - SPACER_FULL_BLOCKS + 1) * SPACER_SHRINK
      if (shrunk <= 0) break
      result.push(shrunk)
    }
  }
  return result
}
