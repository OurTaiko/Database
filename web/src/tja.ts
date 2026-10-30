import type { Difficulty } from '@sitekit/api/types'
export type { Side, Difficulty, DifficultyConstants } from '@sitekit/api/types'

/** Largest TJA the site previews, matching the API's upload limit. */
export const maxTja = 2 * 1024 * 1024
export const maxAudio = 100 * 1024 * 1024
export const maxMovie = 64 * 1024 * 1024

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
