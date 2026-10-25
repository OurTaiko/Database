import { blockKey, sides } from './courses'
import type { Difficulty } from './tja'

/** The block a chart page opens on: the hardest usual course, single first. */
export function defaultDifficulty(difficulties: Difficulty[]): string {
  for (const course of ['Oni', 'Edit', 'Hard', 'Normal', 'Easy'])
    for (const side of sides) {
      const found = difficulties.find((d) => d.course === course && d.side === side)
      if (found) return blockKey(found)
    }
  return ''
}

/** Whether any block of the chart has difficulty constants, so the page can leave the panel out. */
export function hasConstants(difficulties: Difficulty[]): boolean {
  return difficulties.some((d) => (d.constants?.length ?? 0) > 0)
}
