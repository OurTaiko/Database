// OurTaikoSiteKit's API types, then this site's Chart, User and Category and the types built on them.
import type {
  CategoryBase,
  ChartBase,
  ChartList as SharedChartList,
  MyPlaylists as SharedMyPlaylists,
  Playlist as SharedPlaylist,
  PlaylistPage as SharedPlaylistPage,
  Session as SharedSession,
  UserBase,
} from '@sitekit/api/types'
import type { Metadata } from '../tja'

export * from '@sitekit/api/types'
export type User = UserBase & {
  /** users.permission_flags; bit 0 is the upload permission. */
  permissionFlags: number
  canUpload: boolean
}
export type Session = SharedSession<User>
export type Category = CategoryBase & { chartCount: number }
export type CategoryList = { items: Category[]; chartCount: number }
export type Chart = Metadata &
  ChartBase & {
    nijiiroId: number | null
    officialId: string | null
    tjaSize: number
    /** MOVIEOFFSET in seconds. */
    movieOffset?: number
    /** False for a song registered without a TJA and audio: no blocks, no duration, empty file fields. */
    hasFiles: boolean
  }
export type ChartList = SharedChartList<Chart>
export type Playlist = SharedPlaylist<Chart>
export type PlaylistPage = SharedPlaylistPage<Chart>
export type MyPlaylists = SharedMyPlaylists<Chart>
