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
