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
  tjaSize: number
  audioHash: string
  audioSize: number
  coverHash?: string
  /** Transcoded background movie; absent when the chart has none (as on Fanmade). */
  movieHash?: string
  movieSize?: number
  /** MOVIEOFFSET in seconds. */
  movieOffset?: number
  /** False for a song registered without a TJA and audio: no blocks, no duration, empty file fields. */
  hasFiles: boolean
  titleTranslations: Partial<Record<Locale, string>>
  subtitleTranslations: Partial<Record<Locale, string>>
  score: number
  upvotes: number
  downvotes: number
  commentCount: number
  /** Only filled on the signed-in detail view. */
  myVote: Vote
}
export type Vote = -1 | 0 | 1
export type VoteResult = { score: number; upvotes?: number; downvotes?: number; myVote: Vote }
export type CommentSort = 'best' | 'top' | 'new' | 'old' | 'controversial'
export type Comment = {
  id: string
  /** The chart the comment is on, or (for a 段位 course) the course. */
  chartId?: number
  chartTitle?: string
  danId?: string
  danTitle?: Record<string, string>
  parentId: string | null
  depth: number
  authorId: string
  author: string
  authorAvatarUrl?: string
  body: string
  createdAt: string
  editedAt: string | null
  deleted: boolean
  removed: boolean
  score: number
  myVote: Vote
  replyCount: number
  replies: Comment[]
}
export type CommentPage = {
  items: Comment[]
  total: number
  commentCount: number
  page: number
  pageSize: number
  sort: CommentSort
}
export type CommentThread = { item: Comment; sort: CommentSort }
export type CommentList = { items: Comment[]; total: number; page: number; pageSize: number }
export type NotificationKind =
  | 'comment_reply'
  | 'chart_comment'
  | 'chart_upvotes'
  | 'dan_comment'
  | 'dan_upvotes'
  | 'comment_upvotes'
  | 'comment_removed'
export type AppNotification = {
  id: string
  // Unknown future kinds still render with a generic message.
  kind: NotificationKind | (string & {})
  createdAt: string
  read: boolean
  actor: { id: string; nickname: string; avatarUrl?: string } | null
  chart: { id: number; title: string } | null
  dan: { id: string; title: Record<string, string> } | null
  comment: { id: string; parentId: string | null; excerpt: string } | null
  data: {
    upvotes?: number
    /** upload_result: outcome of an upload the user did not wait for. */
    status?: 'published' | 'updated' | 'failed' | (string & {})
    title?: string
    code?: string
    failedFiles?: { file: string; code: string }[]
    /** playlist_removed: the deleted playlist's name. */
    name?: string
  } & Record<string, unknown>
}
export type NotificationPage = {
  items: AppNotification[]
  total: number
  unread: number
  page: number
  pageSize: number
}
export type NotificationSetting = { kind: NotificationKind | (string & {}); enabled: boolean }
export type LeaderboardEntry = {
  id: string
  userId: string
  nickname: string
  avatarUrl?: string
  rank: number
  score: number
  good: number
  ok: number
  bad: number
  drumroll: number
  submittedAt: string
}
export type Leaderboard = {
  songId: number
  /** The ranked chart block; `course` and `side` describe it. */
  difficultyId: number
  course: string
  side: string
  items: LeaderboardEntry[]
  total: number
  page: number
  pageSize: number
}
export type PublicUser = {
  id: string
  nickname: string | null
  avatarUrl?: string
  firstLoginAt: string | null
  lastActiveAt: string | null
  /** Published charts the user uploaded. */
  chartCount: number
  scoreCount: number
  commentCount: number
  karma: number
  /** Public playlists only. */
  playlistCount: number
}
export type UserDirectory = {
  items: PublicUser[]
  total: number
  page: number
  pageSize: number
  profilesAvailable: boolean
}
export type UserSpace = { user: PublicUser; profilesAvailable: boolean }
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
  /** The block the course plays: a chart, a course and a side. */
  chart: {
    id: string | number
    course: 'Easy' | 'Normal' | 'Hard' | 'Oni' | 'Edit'
    side: 'single' | 'p1' | 'p2'
  }
  title: string
  subtitle: string
  hidden: boolean
  level: number
  /** False once the block's content changed or went, until the maker picks a block again. */
  valid: boolean
  /** Valid and the chart still published: the game may challenge it. */
  available: boolean
}
export type Dan = {
  id: string
  title: Record<string, string>
  color: number
  /** Six capital letters or digits, unique on the server: the game's 外伝 entry takes it. */
  code: string
  official: boolean
  songs: DanSong[]
  exams: DanExam[]
  rewards: { dan?: number; titles?: DanRewardTitle[] }
  ownerId: string
  owner: string
  createdAt: string
  updatedAt: string
  score: number
  upvotes: number
  downvotes: number
  commentCount: number
  /** Only filled on the detail page. */
  myVote: Vote
}
export type DanInput = {
  title: Record<string, string>
  color: number
  /** Chosen on creation; left out for a random one. */
  code?: string
  songs: { chartId: string; course: string; side: string; hidden: boolean }[]
  exams: DanExam[]
  rewards: { dan?: number; titles?: DanRewardTitle[] }
}
