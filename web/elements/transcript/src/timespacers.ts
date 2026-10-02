// Discrete time spacers for temporal alignment across columns.
// First 3 blocks: 18px each, 1h per block.
// From block 4: size shrinks by 1px, duration doubles each block.
// Cut off at 5px (block 16+).

const SPACER_PX = 18
const FULL_BLOCKS = 3
const BASE_HOURS = 1
const MIN_PX = 5

export function timeSpacers(prevTs: number, curTs: number): number[] {
  if (prevTs === 0) return []
  let remaining = Math.max(0, curTs - prevTs) / (60 * 60 * 1000) // hours
  const result: number[] = []
  let block = 1
  let blockHours = BASE_HOURS

  while (remaining >= blockHours) {
    const px = block <= FULL_BLOCKS ? SPACER_PX : SPACER_PX - (block - FULL_BLOCKS)
    if (px <= MIN_PX) break
    result.push(px)
    remaining -= blockHours
    block++
    if (block > FULL_BLOCKS) blockHours *= 2
  }

  return result
}
