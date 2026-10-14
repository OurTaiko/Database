/** Largest TJA the site previews, matching the API's upload limit. */
export const maxTja = 2 * 1024 * 1024
export const maxAudio = 100 * 1024 * 1024

export type Side = 'single' | 'p1' | 'p2'

/**
 * One chart block: a slot of course × side, as the arcade machine keeps a
 * single, a 1P and a 2P chart per course. Only blocks the stored TJA really
 * contains are listed; a player without a block of their own falls back:
 * single play single → p1; two players, 1P p1 → single; 2P p2 → single → p1.
 */
export type Difficulty = {
  /** chart_difficulties row. */
  id?: number
  /** Easy, Normal, Hard, Oni or Edit. */
  course: string
  side: Side
  level: number
  maker: string
  branching?: boolean
}

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

export function isAudioFilename(name: string) {
  return /\.(ogg|mp3)$/i.test(name)
}
