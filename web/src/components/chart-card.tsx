import { useTranslation } from 'react-i18next'
import { FileDashedIcon } from '@phosphor-icons/react'
import type { Chart } from '@/api/types'
import { Badge } from '@sitekit/components/ui/badge'
import {
  BpmBadge,
  BranchingBadge,
  ChartCard as SharedChartCard,
  CommentCountBadge,
  DoubleBadge,
  DurationBadge,
  ScoreBadge,
} from '@sitekit/components/chart-card'

export function ChartCard({ chart }: { chart: Chart }) {
  const { t } = useTranslation()
  return (
    <SharedChartCard
      chart={chart}
      facts={
        <>
          <BpmBadge bpm={chart.bpm} />
          {chart.hasFiles ? (
            <DurationBadge seconds={chart.duration} />
          ) : (
            <li>
              <Badge variant="outline" className="text-muted-foreground">
                <FileDashedIcon className="size-3" aria-hidden="true" />
                {t('messages.noFiles')}
              </Badge>
            </li>
          )}
        </>
      }
      footer={
        <>
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
          <DoubleBadge difficulties={chart.difficulties} />
          <BranchingBadge difficulties={chart.difficulties} />
          <ScoreBadge score={chart.score} />
          <CommentCountBadge count={chart.commentCount} />
        </>
      }
    />
  )
}
