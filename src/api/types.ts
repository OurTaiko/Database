import type { Metadata } from '../tja'

export type User = {
  id: string
  username: string
  nickname: string
  emailVerified: boolean
  isAdmin: boolean
  preferredLanguage?: string
  avatarUrl?: string
  /** users.permission_flags; bit 0 is the upload permission. */
  permissionFlags: number
  canUpload: boolean
}
export type Locale = 'en' | 'ja' | 'zh' | 'ko'
export type Session = { user: User | null; csrfToken: string }
export type Category = { id: string; title: string; genre: string; chartCount: number }
export type CategoryList = { items: Category[]; chartCount: number }
export type Chart = Metadata & {
  /** Auto-incremented database ID. */
  id: number
  nijiiroId: number | null
  officialId: string | null
  uploaderId: string
  uploader: string
  uploaderAvatarUrl?: string
  description: string
  createdAt: string
  /** Latest change of the chart or anything related to it. */
  updateAt: string
  duration: number
  demoEnd: number
  categoryIds: string[]
  tjaName: string
  audioName: string
  tjaHash: string
  tjaSize: number
  audioHash: string
  audioSize: number
  coverHash?: string
  /** False for a song registered without a TJA and audio: no blocks, no duration, empty file fields. */
  hasFiles: boolean
  titleTranslations: Partial<Record<Locale, string>>
  subtitleTranslations: Partial<Record<Locale, string>>
}
export type ChartList = { items: Chart[]; total: number; page: number; pageSize: number }

/** A playlist's ID is a decimal number, or a word for a system playlist, which
 * the server computes: no owner, read-only. */
export type Playlist = {
  id: string
  name: string
  description: string
  public: boolean
  system: boolean
  categoryId: string
  genre: string
  ownerId?: string
  owner: { id: string; nickname: string; avatarUrl?: string } | null
  coverHash?: string
  chartCount: number
  favoriteCount: number
  favorited: boolean
  createdAt: string | null
  updatedAt: string | null
  /** Only in the viewer's own list when it asks about one chart. */
  containsChart?: boolean
  /** Only in the detail response, in playlist order. */
  charts?: Chart[]
}
export type PlaylistPage = { items: Playlist[]; total: number; page: number; pageSize: number }
export type MyPlaylists = {
  owned: Playlist[]
  favorites: Playlist[]
  maxPlaylists: number
  maxItems: number
}

// 段位 courses (OurTaikoPlay's course format, as GET /api/v1/dans returns them).
export type DanExamType = 'gauge' | 'good' | 'ok' | 'bad' | 'hits' | 'score' | 'combo' | 'rolls'
export type DanExam = {
  type: DanExamType
  range: 'more' | 'less'
  // [red] or [red, gold] for the course, or one such (or null) per song.
  value: number[] | (number[] | null)[]
}
export type DanRewardTitle = {
  id: string
  on: 'red' | 'red1' | 'red2' | 'red3' | 'gold' | 'gold4' | 'gold5' | 'gold6'
}
export type DanSong = {
  chart: {
    id: string | number
    course: 'Easy' | 'Normal' | 'Hard' | 'Oni' | 'Edit'
    tjaHash: string
  }
  title: string
  subtitle: string
  hidden: boolean
  level: number
  available: boolean
}
export type Dan = {
  id: string
  title: Record<string, string>
  color: number
  official: boolean
  songs: DanSong[]
  exams: DanExam[]
  rewards: { dan?: number; titles?: DanRewardTitle[] }
  ownerId: string
  owner: string
  createdAt: string
  updatedAt: string
}
export type DanInput = {
  title: Record<string, string>
  color: number
  songs: { chartId: string; course: string; hidden: boolean }[]
  exams: DanExam[]
  rewards: { dan?: number; titles?: DanRewardTitle[] }
}
