import type { Difficulty } from '@sitekit/api/types'
export type { Side, Difficulty, DifficultyConstants } from '@sitekit/api/types'

export { maxTja, maxAudio, maxMovie } from '@sitekit/tja-limits'

/** What the database reads from a stored TJA. */
export type Metadata = {
  title: string
  subtitle: string
  maker: string
  bpm: number
  offset: number
  demoStart: number
  /** The stored audio's file name: `{sha256}.{extension}`. */
  wave: string
  difficulties: Difficulty[]
}

export const isMovieFilename = (name: string) => /\.(mp4|webm|mov)$/i.test(name)

export function isAudioFilename(name: string) {
  return /\.(ogg|mp3)$/i.test(name)
}
