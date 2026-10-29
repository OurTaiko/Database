/** Largest TJA the site previews, matching the API's upload limit. */
export const maxTja = 2 * 1024 * 1024
export const maxAudio = 100 * 1024 * 1024
export const maxMovie = 64 * 1024 * 1024

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
  /**
   * The server's content hash of the block as played ("v1:" + 64 hex): the block's identity for
   * scores and 段位 courses. Null until the server has computed it; absent in the browser's own parse.
   */
  chartHash?: string | null
  /** The block's difficulty constants by route, in a chart's detail only (none until rated). */
  constants?: DifficultyConstants[]
}
/** One route's difficulty constants (定数): the main and 75/99 sub-constants and five dimensions, 0–15.5. */
export type DifficultyConstants = {
  route: 'unbranched' | 'normal' | 'expert' | 'master'
  mainConstant: number
  subConstant75: number
  subConstant99: number
  stamina: number
  handspeed: number
  burst: number
  complex: number
  rhythm: number
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

export const isMovieFilename = (name: string) => /\.(mp4|webm|mov)$/i.test(name)

export function isAudioFilename(name: string) {
  return /\.(ogg|mp3)$/i.test(name)
}
