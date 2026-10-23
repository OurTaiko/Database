// OurTaikoProfile (profile.ourtaiko.org): every player's scores of both servers, titles and 段位.
export const PROFILE_URL = (
  (import.meta.env.VITE_PROFILE_URL as string | undefined) || 'https://profile.ourtaiko.org'
).replace(/\/$/, '')
