import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { api } from '@sitekit/api/client'
import { endpoints } from '@/api/endpoints'
import type { Chart, Leaderboard } from '@/api/types'
import { formatLocale } from '@/i18n'
import { Button } from '@sitekit/components/ui/button'
import { UserAvatar } from '@sitekit/components/user-avatar'
import { ProfilePlate } from '@sitekit/components/profile-plate'
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@sitekit/components/ui/table'
import { baseCourse, slotKey, courseNames } from './courses'
import { Notice } from '@sitekit/notifications'
import { useSession } from './session-context'

// One block's leaderboard (ported from Fanmade): every player once, with their
// best play on the chart's current files.
export function ChartLeaderboard({ chart, course }: { chart: Chart; course: string }) {
  const { t, i18n } = useTranslation()
  const [page, setPage] = useState(1)
  const [data, setData] = useState<Leaderboard | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const { user } = useSession()
  const { id } = chart
  const difficultyId = chart.difficulties.find((d) => slotKey(d) === course)?.id
  const locale = formatLocale(i18n.resolvedLanguage)
  useEffect(() => {
    const controller = new AbortController()
    setData(null)
    setError('')
    api<Leaderboard>(
      endpoints.leaderboard(id, { ...(difficultyId ? { difficultyId } : {}), page }),
      {
        signal: controller.signal,
      },
    )
      .then((response) => {
        if (!controller.signal.aborted) setData(response)
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(e.message)
      })
    return () => controller.abort()
  }, [id, difficultyId, page, attempt])
  if (error)
    return (
      <div className="flex min-h-40 flex-col items-center justify-center gap-3 py-6 text-center">
        <Notice>{error}</Notice>
        <Button variant="outline" size="default" onClick={() => setAttempt((n) => n + 1)}>
          {t('messages.retry')}
        </Button>
      </div>
    )
  if (!data)
    return (
      <p
        className="flex min-h-40 flex-col items-center justify-center gap-3 py-6 text-center text-sm text-muted-foreground"
        role="status"
      >
        {t('messages.loadingLeaderboard')}
      </p>
    )
  const pages = Math.max(1, Math.ceil(data.total / data.pageSize))
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2 text-sm [&>span]:text-muted-foreground">
        <strong>{t('common.playerCount', { count: data.total })}</strong>
        <span>{t('messages.bestScoresTiesShareRanks')}</span>
      </div>
      {data.items.length === 0 ? (
        <div className="flex min-h-40 flex-col items-center justify-center gap-3 py-6 text-center">
          <h3 className="text-base font-extrabold">
            {data.total ? t('messages.noScoresOnThisPage') : t('messages.noScoresYet')}
          </h3>
          <p className="text-sm text-muted-foreground">
            {t('messages.playThisDifficultyAndSubmitAScoreToAppearHere')}
          </p>
        </div>
      ) : (
        <div
          className="min-w-0 overflow-x-auto rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring"
          tabIndex={0}
          role="region"
          aria-label={t('messages.scoreRankings')}
        >
          <Table>
            <TableCaption className="sr-only">
              {t('messages.difficultyLeaderboard', {
                difficulty: courseNames[baseCourse(course)] ?? course,
              })}
            </TableCaption>
            <TableHeader>
              <TableRow>
                {[
                  t('messages.rank'),
                  t('messages.player'),
                  t('messages.score'),
                  t('messages.good'),
                  t('messages.ok'),
                  t('messages.bad'),
                  t('messages.drumroll'),
                  t('messages.submitted'),
                ].map((label) => (
                  <TableHead scope="col" key={label}>
                    {label}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.items.map((entry) => (
                <TableRow
                  key={entry.id}
                  className={entry.userId === user?.id ? 'bg-accent/50' : ''}
                >
                  <TableCell>
                    <span className="font-medium tabular-nums">#{entry.rank}</span>
                  </TableCell>
                  <TableHead scope="row">
                    <Link
                      className="inline-flex max-w-full items-center gap-2 align-middle text-link hover:underline"
                      to={`/users/${encodeURIComponent(entry.userId)}`}
                    >
                      <UserAvatar
                        nickname={entry.nickname}
                        avatarUrl={entry.avatarUrl}
                        className="size-6"
                        fallbackClassName="text-xs"
                      />
                      <span className="truncate">{entry.nickname}</span>
                    </Link>
                    <ProfilePlate userId={entry.userId} className="ml-2" />
                    {entry.userId === user?.id && (
                      <span className="ml-2 rounded-md bg-secondary px-1.5 py-0.5 text-xs text-secondary-foreground">
                        {t('messages.you')}
                      </span>
                    )}
                  </TableHead>
                  <TableCell className="font-semibold tabular-nums">
                    {entry.score.toLocaleString(locale)}
                  </TableCell>
                  <TableCell>{entry.good.toLocaleString(locale)}</TableCell>
                  <TableCell>{entry.ok.toLocaleString(locale)}</TableCell>
                  <TableCell>{entry.bad.toLocaleString(locale)}</TableCell>
                  <TableCell>{entry.drumroll.toLocaleString(locale)}</TableCell>
                  <TableCell>
                    <time dateTime={entry.submittedAt}>
                      {new Date(entry.submittedAt).toLocaleString(locale)}
                    </time>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      {pages > 1 && (
        <div className="flex flex-wrap items-center justify-center gap-4 text-sm">
          <Button
            variant="outline"
            size="default"
            disabled={page <= 1}
            onClick={() => setPage((n) => n - 1)}
          >
            {t('messages.previous')}
          </Button>
          <span>{t('messages.pagination', { page, pages })}</span>
          <Button
            variant="outline"
            size="default"
            disabled={page >= pages || page >= 10000}
            onClick={() => setPage((n) => n + 1)}
          >
            {t('messages.next')}
          </Button>
        </div>
      )}
    </div>
  )
}
