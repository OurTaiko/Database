import { chartText } from '@/chart-language'
import { i18n } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import type { Chart } from '@/api/types'
import { coverSource } from '@/cover'
import { CategoryLabels } from '@/categories'
import { supportsChart } from '@/courses'
import { Badge } from '@sitekit/components/ui/badge'
import { Card, CardContent } from '@sitekit/components/ui/card'
import { DifficultyBadges } from '@/components/difficulty-badges'
import {
  ArrowFatUpIcon,
  ChatCircleIcon,
  FileDashedIcon,
  GitBranchIcon,
  UsersIcon,
} from '@phosphor-icons/react'
import { compactNumber } from '@/components/vote-control'
import { cn } from '@sitekit/lib/utils'
import { categoryStripeColor } from '@sitekit/category-colors'
import {
  CardCoverBackdrop,
  coveredTextClassName,
  useCardCover,
} from '@sitekit/components/card-cover'

export function formatDuration(seconds: number) {
  return `${Math.floor(seconds / 60)}:${String(Math.floor(seconds % 60)).padStart(2, '0')}`
}
export function ChartCard({ chart }: { chart: Chart }) {
  const { t } = useTranslation()
  const cover = useCardCover(coverSource(chart))
  const hasCover = !!cover.source
  if (!supportsChart(chart)) return null
  const text = chartText(chart, i18n.resolvedLanguage)
  return (
    <Link
      className="group block rounded-[14px] outline-none focus-visible:ring-ring focus-visible:ring-3 focus-visible:ring-offset-4 min-w-0 hover:-translate-y-0.5 motion-reduce:transform-none transition-transform motion-reduce:transition-none duration-150"
      data-testid="chart-card"
      to={`/charts/${chart.id}`}
    >
      {/* Fixed height: five difficulty bookmarks, the most a song has, fit exactly. */}
      <Card
        size="sm"
        className="isolate relative group-hover:shadow-drop-hover pt-6 pb-5 h-56 transition-shadow"
      >
        <div
          aria-hidden="true"
          className="top-0 z-10 absolute inset-x-0 border-b-2 border-outline h-1.5"
          style={{ background: categoryStripeColor(chart.categoryIds) }}
        />
        {cover.source && <CardCoverBackdrop source={cover.source} onError={cover.onError} />}
        <CardContent className="relative flex gap-4 pr-4 pl-6 h-full min-h-0">
          <div className="flex flex-col flex-1 gap-4 min-w-0 min-h-0">
            <div className={cn('space-y-0.5 min-w-0', hasCover && coveredTextClassName)}>
              <h3 className="font-extrabold text-lg truncate" title={text.title}>
                {text.title}
              </h3>
              <p
                className={cn(
                  'text-sm truncate',
                  hasCover ? 'text-foreground/80' : 'text-muted-foreground',
                )}
                title={text.subtitle}
              >
                {text.subtitle}
              </p>
            </div>
            {/* Two rows of facts at most; a third row is cut off whole. */}
            <div className="max-h-[3.25rem] overflow-hidden">
              <CategoryLabels ids={chart.categoryIds}>
                {chart.bpm > 0 && (
                  <li>
                    <Badge variant="outline" className="tabular-nums">
                      {chart.bpm} BPM
                    </Badge>
                  </li>
                )}
                {chart.hasFiles ? (
                  <li>
                    <Badge variant="outline" className="tabular-nums">
                      {formatDuration(chart.duration)}
                    </Badge>
                  </li>
                ) : (
                  <li>
                    <Badge variant="outline" className="text-muted-foreground">
                      <FileDashedIcon className="size-3" aria-hidden="true" />
                      {t('db.noFiles')}
                    </Badge>
                  </li>
                )}
              </CategoryLabels>
            </div>
            <ul
              className="flex items-center gap-2 mt-auto"
              aria-label={t('messages.chartInformation')}
            >
              <li>
                {chart.nijiiroId === null ? (
                  <Badge variant="outline" className="text-muted-foreground tabular-nums">
                    #{chart.numberId}
                  </Badge>
                ) : (
                  <Badge
                    variant="outline"
                    className="gap-0 p-0 tabular-nums"
                    title={t('db.nijiiroIdWithValue', { id: chart.nijiiroId })}
                  >
                    <span className="flex items-center bg-muted px-2 border-r h-full text-muted-foreground shrink-0">
                      Nijiiro
                    </span>
                    <span className="px-2">{chart.nijiiroId}</span>
                  </Badge>
                )}
              </li>
              {chart.difficulties.some((d) => d.side !== 'single') && (
                <li>
                  <Badge variant="outline" title={t('db.hasDouble')}>
                    <UsersIcon className="size-3" aria-hidden="true" />
                    <span className="sr-only">{t('db.hasDouble')}</span>
                  </Badge>
                </li>
              )}
              {chart.difficulties.some((d) => d.branching) && (
                <li>
                  <Badge variant="outline" title={t('db.hasBranching')}>
                    <GitBranchIcon className="size-3" aria-hidden="true" />
                    <span className="sr-only">{t('db.hasBranching')}</span>
                  </Badge>
                </li>
              )}
              <li>
                <Badge
                  variant="outline"
                  className="tabular-nums"
                  title={t('interactions.scoreExact', { score: chart.score })}
                >
                  <ArrowFatUpIcon className="size-3" aria-hidden="true" />
                  <span className="sr-only">{t('interactions.score')}</span>
                  {compactNumber(chart.score, i18n.resolvedLanguage ?? '')}
                </Badge>
              </li>
              <li>
                <Badge
                  variant="outline"
                  className="tabular-nums"
                  title={t('interactions.commentCount', { count: chart.commentCount })}
                >
                  <ChatCircleIcon className="size-3" aria-hidden="true" />
                  <span className="sr-only">
                    {t('interactions.commentCount', { count: chart.commentCount })}
                  </span>
                  <span aria-hidden="true">
                    {compactNumber(chart.commentCount, i18n.resolvedLanguage ?? '')}
                  </span>
                </Badge>
              </li>
            </ul>
          </div>
          <DifficultyBadges difficulties={chart.difficulties} />
        </CardContent>
      </Card>
    </Link>
  )
}
