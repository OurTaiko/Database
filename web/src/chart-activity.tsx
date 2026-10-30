import { i18n } from '@sitekit/i18n'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@sitekit/api/endpoints'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@sitekit/components/ui/tabs'
import { cn } from '@sitekit/lib/utils'
import { Button } from '@sitekit/components/ui/button'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import {
  FileAudioIcon as FileMusic,
  GitBranchIcon,
  PlayIcon,
  TrophyIcon,
  StarIcon,
  ArrowCounterClockwiseIcon as RotateCcw,
} from '@phosphor-icons/react'
import type { Chart } from './api/types'
import { baseCourse, slotKey, groupCourses, groupLevel, groupName } from '@sitekit/courses'
import { Notice } from '@sitekit/notifications'
import { maxTja } from './tja'
import { ChartLeaderboard } from './chart-leaderboard'
import { withChartTitles } from '@sitekit/tja-titles'

const ChartPreview = lazy(() => import('./chart-preview'))
const EmbeddedPlayer = lazy(() => import('@sitekit/embedded-player'))

const difficultyTabColors = {
  Easy: '[--difficulty-bg:#f9a15a]',
  Normal: '[--difficulty-bg:#8fd16a]',
  Hard: '[--difficulty-bg:#6cc7d8]',
  Oni: '[--difficulty-bg:#f77fab]',
  Edit: '[--difficulty-bg:#b99ae8]',
}

const tabButtonGroupClassName =
  'shrink-0 gap-0 overflow-hidden rounded-[10px] border-2 border-outline bg-card p-0'
const tabButtonClassName =
  'h-full rounded-none px-3 text-muted-foreground shadow-none not-first:border-l! not-first:border-l-border! hover:bg-muted/50 focus-visible:z-10 focus-visible:-ring-offset-2 data-active:bg-muted data-active:text-foreground dark:hover:bg-muted/50 dark:data-active:bg-muted after:hidden'
const difficultyTabClassName =
  'bg-transparent font-bold text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring data-active:bg-[var(--difficulty-bg)] data-active:text-[#382d2a] data-active:hover:bg-[var(--difficulty-bg)] data-active:hover:text-[#382d2a]'

export type ActivityTab = 'image' | 'preview' | 'leaderboard'

// The chart image of one block, the embedded player playing it, or its leaderboard. A course
// may have a single, a 1P and a 2P chart: one tab per course, then a side
// switch. `course` is the block's key.
// The chosen block is the page's (Detail), so the difficulty constants beside follow it.
export function ChartActivity({
  chart,
  tab,
  onTabChange: setTab,
  course,
  onCourseChange: setCourse,
}: {
  chart: Chart
  tab: ActivityTab
  onTabChange: (tab: ActivityTab) => void
  course: string
  onCourseChange: (course: string) => void
}) {
  const { t } = useTranslation()
  const [source, setSource] = useState('')
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const groups = groupCourses(chart.difficulties)
  const group = groups.find((g) => g.base === baseCourse(course))
  const { id } = chart
  // Reload the text when any block changed (the file behind it did).
  const blockHashes = chart.difficulties.map((d) => d.chartHash ?? '').join(',')
  // The image and the player name the chart as the database does (R042).
  const titled = useMemo(() => (source ? withChartTitles(source, chart) : ''), [source, chart])
  useEffect(() => {
    const controller = new AbortController()
    setSource('')
    setError('')
    async function load() {
      try {
        const response = await fetch(endpoints.resource({ id }, 'tja'), {
          signal: controller.signal,
          credentials: 'omit',
        })
        if (!response.ok)
          throw new Error(i18n.t('messages.chartHttpError', { status: response.status }))
        const bytes = await response.arrayBuffer()
        if (bytes.byteLength > maxTja)
          throw new Error(i18n.t('messages.chartExceedsThePreviewSizeLimit'))
        // Stored TJA files are always UTF-8.
        const text = new TextDecoder('utf-8', { fatal: true }).decode(bytes)
        if (!controller.signal.aborted) setSource(text)
      } catch (e) {
        if (!controller.signal.aborted)
          setError(e instanceof Error ? e.message : i18n.t('messages.failedToLoadChart'))
      }
    }
    void load()
    return () => controller.abort()
  }, [id, blockHashes, attempt])
  const status =
    'flex min-h-40 flex-col items-center justify-center gap-3 py-6 text-center text-sm text-muted-foreground'
  return (
    <Tabs
      value={tab}
      onValueChange={(value) => setTab(value as ActivityTab)}
      className="min-w-0 gap-5 rounded-[14px] border-2 border-outline bg-card p-5 shadow-drop sm:p-6"
      aria-label={t('messages.chartPreviewAndLeaderboard')}
      data-testid="chart-activity"
    >
      <section
        className="flex min-w-0 flex-wrap items-center justify-between gap-3"
        aria-label={t('messages.chartContentAndDifficultySelection')}
      >
        <TabsList
          aria-label={t('messages.songDetails')}
          className={cn(tabButtonGroupClassName, tab === 'leaderboard' && 'max-lg:hidden')}
        >
          <TabsTrigger value="image" className={tabButtonClassName}>
            <FileMusic size={18} aria-hidden="true" />
            {t('messages.chartImage')}
          </TabsTrigger>
          <TabsTrigger value="preview" className={tabButtonClassName}>
            <PlayIcon size={18} aria-hidden="true" />
            {t('messages.chartPreview')}
          </TabsTrigger>
          {/* Below lg the page tabs open the leaderboard. */}
          <TabsTrigger value="leaderboard" className={cn(tabButtonClassName, 'max-lg:hidden')}>
            <TrophyIcon size={18} aria-hidden="true" />
            {t('messages.leaderboard')}
          </TabsTrigger>
        </TabsList>
        <div className="-mx-1 flex min-w-0 max-w-full items-center gap-3 overflow-x-auto px-1 py-1">
          <Tabs
            value={group?.base ?? ''}
            onValueChange={(value) => {
              const next = groups.find((g) => g.base === value)
              if (!next) return
              // Keep the chosen side when the next course has it too.
              const side = course.slice(baseCourse(course).length)
              setCourse(
                slotKey(
                  next.sides.find((d) => slotKey(d) === `${next.base}${side}`) ?? next.sides[0],
                ),
              )
            }}
            className="shrink-0"
          >
            <TabsList
              aria-label={t('messages.selectDifficulty')}
              className={tabButtonGroupClassName}
            >
              {groups.map((g) => (
                <TabsTrigger
                  key={g.base}
                  value={g.base}
                  className={`${tabButtonClassName} ${difficultyTabClassName} ${difficultyTabColors[g.base]}`}
                >
                  {groupName(g)}
                  <span
                    className="inline-flex items-center gap-1 tabular-nums"
                    aria-label={g.sides
                      .map((d) => t('common.stars', { count: d.level }))
                      .filter((label, index, all) => all.indexOf(label) === index)
                      .join(' / ')}
                  >
                    <StarIcon weight="fill" className="size-3" aria-hidden="true" />
                    {groupLevel(g)}
                  </span>
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          {group && group.sides.length > 1 && (
            <Tabs
              value={course}
              onValueChange={(value) => setCourse(String(value))}
              className="shrink-0"
            >
              <TabsList
                aria-label={t('messages.selectPlayerSide')}
                className={tabButtonGroupClassName}
              >
                {group.sides.map((d) => (
                  <TabsTrigger key={d.side} value={slotKey(d)} className={tabButtonClassName}>
                    {d.side === 'single'
                      ? t('messages.singleSide')
                      : d.side.slice(1).toUpperCase() + 'P'}
                    {d.branching && (
                      <GitBranchIcon className="size-3" aria-label={t('messages.hasBranching')} />
                    )}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          )}
        </div>
      </section>
      <div className="min-w-0">
        {/* The leaderboard does not need the TJA. */}
        {tab === 'leaderboard' ? (
          <TabsContent value="leaderboard">
            <ChartLeaderboard key={course} chart={chart} course={course} />
          </TabsContent>
        ) : error ? (
          <div className="flex min-h-40 flex-col items-center justify-center gap-3 py-6 text-center">
            <Notice>{error}</Notice>
            <Button variant="outline" size="default" onClick={() => setAttempt((n) => n + 1)}>
              <RotateCcw size={16} />
              {t('messages.reload')}
            </Button>
          </div>
        ) : !source ? (
          <p className={status} role="status">
            {t('messages.loadingTjaFromServer')}
          </p>
        ) : (
          <>
            <TabsContent value="image">
              {tab === 'image' && (
                <Suspense
                  fallback={
                    <p className={status} role="status">
                      {t('messages.loadingChartPreview')}
                    </p>
                  }
                >
                  <ChartPreview key={course} chart={chart} source={titled} course={course} />
                </Suspense>
              )}
            </TabsContent>
            <TabsContent value="preview">
              {tab === 'preview' && (
                <Suspense
                  fallback={
                    <p className={status} role="status">
                      {t('messages.loadingChartPreview')}
                    </p>
                  }
                >
                  <EmbeddedPlayer chart={chart} source={titled} course={course} />
                </Suspense>
              )}
            </TabsContent>
          </>
        )}
      </div>
    </Tabs>
  )
}
