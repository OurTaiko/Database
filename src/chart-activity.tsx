import { i18n } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Button } from '@/components/ui/button'
import { lazy, Suspense, useEffect, useState } from 'react'
import {
  FileAudioIcon as FileMusic,
  GitBranchIcon,
  PlayIcon,
  StarIcon,
  ArrowCounterClockwiseIcon as RotateCcw,
} from '@phosphor-icons/react'
import type { Chart } from './api/types'
import { baseCourse, blockKey, groupCourses, groupLevel, groupName } from './courses'
import { Notice } from './notifications'
import { defaultDifficulty } from './chart-difficulty'
import { maxTja } from './tja'

const ChartPreview = lazy(() => import('./chart-preview'))
const EmbeddedPlayer = lazy(() => import('./embedded-player'))

const difficultyTabColors = {
  Easy: '[--difficulty-bg:var(--color-orange-100)] [--difficulty-fg:var(--color-orange-800)] dark:[--difficulty-bg:#493128] dark:[--difficulty-fg:#f7bb78]',
  Normal:
    '[--difficulty-bg:var(--color-green-100)] [--difficulty-fg:var(--color-green-800)] dark:[--difficulty-bg:#263e32] dark:[--difficulty-fg:#93d5a6]',
  Hard: '[--difficulty-bg:var(--color-yellow-100)] [--difficulty-fg:var(--color-yellow-800)] dark:[--difficulty-bg:#393a21] dark:[--difficulty-fg:#d7d887]',
  Oni: '[--difficulty-bg:var(--color-purple-100)] [--difficulty-fg:var(--color-purple-800)] dark:[--difficulty-bg:#3f2948] dark:[--difficulty-fg:#deb0ed]',
  Edit: '[--difficulty-bg:var(--color-rose-100)] [--difficulty-fg:var(--color-rose-800)] dark:[--difficulty-bg:#462733] dark:[--difficulty-fg:#f19aae]',
}

const tabButtonGroupClassName =
  'shrink-0 gap-0 overflow-hidden rounded-full border border-border bg-transparent p-0'
const tabButtonClassName =
  'h-full rounded-none px-3 text-muted-foreground shadow-none not-first:border-l! not-first:border-l-border! hover:bg-muted/50 focus-visible:z-10 focus-visible:-ring-offset-2 data-active:bg-muted data-active:text-foreground dark:hover:bg-muted/50 dark:data-active:bg-muted after:hidden'
const difficultyTabClassName =
  'bg-transparent text-[color-mix(in_oklab,var(--difficulty-fg)_65%,transparent)] hover:bg-transparent hover:text-[var(--difficulty-fg)] focus-visible:ring-[var(--difficulty-fg)] data-active:hover:bg-[var(--difficulty-bg)] data-active:bg-[var(--difficulty-bg)] data-active:text-[var(--difficulty-fg)] dark:bg-transparent dark:text-[color-mix(in_oklab,var(--difficulty-fg)_65%,transparent)] dark:hover:bg-transparent dark:hover:text-[var(--difficulty-fg)] dark:data-active:hover:bg-[var(--difficulty-bg)] dark:data-active:bg-[var(--difficulty-bg)] dark:data-active:text-[var(--difficulty-fg)]'

export type ActivityTab = 'image' | 'preview'

// The chart image of one block, or the embedded player playing it. A course
// may have a single, a 1P and a 2P chart: one tab per course, then a side
// switch. `course` is the block's key.
export function ChartActivity({ chart }: { chart: Chart }) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<ActivityTab>('image')
  const [course, setCourse] = useState(() => defaultDifficulty(chart.difficulties))
  const [source, setSource] = useState('')
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const groups = groupCourses(chart.difficulties)
  const group = groups.find((g) => g.base === baseCourse(course))
  const { id, tjaHash } = chart
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
  }, [id, tjaHash, attempt])
  const status =
    'flex min-h-40 flex-col items-center justify-center gap-3 py-6 text-center text-sm text-muted-foreground'
  return (
    <Tabs
      value={tab}
      onValueChange={(value) => setTab(value as ActivityTab)}
      className="min-w-0 gap-5 rounded-2xl bg-white p-5 shadow-[0_4px_12px_rgba(0,0,0,0.08)] sm:p-6 dark:bg-card"
      aria-label={t('messages.chartSections')}
      data-testid="chart-activity"
    >
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
        <TabsList aria-label={t('messages.songDetails')} className={tabButtonGroupClassName}>
          <TabsTrigger value="image" className={tabButtonClassName}>
            <FileMusic size={18} aria-hidden="true" />
            {t('messages.chartImage')}
          </TabsTrigger>
          <TabsTrigger value="preview" className={tabButtonClassName}>
            <PlayIcon size={18} aria-hidden="true" />
            {t('messages.chartPreview')}
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
                blockKey(
                  next.sides.find((d) => blockKey(d) === `${next.base}${side}`) ?? next.sides[0],
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
                  <TabsTrigger key={d.side} value={blockKey(d)} className={tabButtonClassName}>
                    {d.side === 'single' ? t('db.single') : d.side.slice(1).toUpperCase() + 'P'}
                    {d.branching && (
                      <GitBranchIcon className="size-3" aria-label={t('db.hasBranching')} />
                    )}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          )}
        </div>
      </div>
      <div className="min-w-0">
        {error ? (
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
                  <ChartPreview key={course} chart={chart} source={source} course={course} />
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
                  <EmbeddedPlayer chart={chart} source={source} course={course} />
                </Suspense>
              )}
            </TabsContent>
          </>
        )}
      </div>
    </Tabs>
  )
}
