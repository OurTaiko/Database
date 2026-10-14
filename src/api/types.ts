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
  titleTranslations: Partial<Record<Locale, string>>
  subtitleTranslations: Partial<Record<Locale, string>>
}
export type ChartList = { items: Chart[]; total: number; page: number; pageSize: number }
