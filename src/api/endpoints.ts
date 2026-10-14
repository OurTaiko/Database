import { API_ROOT } from './config'
import type { Chart } from './types'

function withQuery(url: string, values: Record<string, string | number>): string {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(values)) query.set(key, String(value))
  return query.size ? `${url}?${query}` : url
}
const chartPath = (id: number | string) => `${API_ROOT}/charts/${encodeURIComponent(id)}`

/** Filters of the chart list; an empty value does not filter. */
export type ChartQuery = {
  q: string
  page: number
  order?: string
  category?: string
  course?: string
  level?: string
  nijiiro?: string
}

export const endpoints = {
  me: `${API_ROOT}/me`,
  categories: `${API_ROOT}/categories`,
  uploadRules: `${API_ROOT}/upload-rules`,
  logout: `${API_ROOT}/auth/logout`,
  login: (returnTo: string) => withQuery(`${API_ROOT}/auth/sso/login`, { returnTo }),
  accountRegister: `${API_ROOT}/auth/account/register`,
  accountProfile: `${API_ROOT}/auth/account/profile`,
  charts: `${API_ROOT}/charts`,
  chartList: (query: ChartQuery, mine = false) =>
    withQuery(
      `${API_ROOT}${mine ? '/me/charts' : '/charts'}`,
      Object.fromEntries(Object.entries(query).filter(([, value]) => value !== '')),
    ),
  chart: chartPath,
  cover: (id: number, hash?: string) =>
    withQuery(`${chartPath(id)}/cover`, hash ? { v: hash } : {}),
  resource: (chart: Pick<Chart, 'id'>, kind: 'audio' | 'tja' | 'download') =>
    `${chartPath(chart.id)}/${kind}`,
} as const
