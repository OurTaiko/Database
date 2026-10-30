import { chartPath, endpoints as shared } from '@sitekit/api/endpoints'
import type { Chart } from './types'

// The chart list also takes category, course, level, nijiiro and files filters.
export const endpoints = {
  ...shared,
  resource: (chart: Pick<Chart, 'id'>, kind: 'audio' | 'tja' | 'download' | 'movie') =>
    `${chartPath(chart.id)}/${kind}`,
} as const
