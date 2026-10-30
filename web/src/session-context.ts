import { useSession as useSharedSession } from '@sitekit/session-context'
import type { SessionState as SharedSessionState } from '@sitekit/session-context'
import type { User } from './api/types'

export { SessionContext } from '@sitekit/session-context'
// The site's User, with its upload permission.
export type SessionState = SharedSessionState<User>
export const useSession = useSharedSession<User>
