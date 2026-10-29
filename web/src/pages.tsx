import { formatLocale } from '@/i18n'
import { chartText } from '@/chart-language'
import { i18n } from '@/i18n'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { DialogClose } from '@/components/ui/dialog'
import { ChartCover, CoverDialog } from '@/components/chart-cover'
import { AudioPlayer } from '@/components/audio-player'
import { buttonVariants, Button } from '@/components/ui/button'
import { ChartSearch, chartOrders, filterCourses } from '@/components/chart-search'
import type { ChartFilters, ChartOrder } from '@/components/chart-search'
import { Skeleton } from '@/components/ui/skeleton'
import { Card } from '@/components/ui/card'
import { UserAvatar } from '@/components/user-avatar'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  ArrowRightIcon as ArrowRight,
  DownloadSimpleIcon as Download,
  ArrowSquareOutIcon,
  CaretRightIcon,
  FileDashedIcon,
  PlaylistIcon,
  UploadSimpleIcon,
  ChatCircleIcon,
} from '@phosphor-icons/react'
import { api, jsonRequest } from './api/client'
import { AddToPlaylistDialog } from './playlist-dialogs'
import type { Chart, ChartList, Vote, VoteResult } from './api/types'
import { ChartCard, formatDuration } from '@/components/chart-card'
import { slotKey, supportsChart } from './courses'
import { defaultDifficulty, hasConstants } from './chart-difficulty'
import { DifficultyConstants } from './difficulty-constants'
import { useSession } from './session-context'
import { EditMetadata } from './edit-metadata'
import { ChartActivity, type ActivityTab } from './chart-activity'
import { CommentSection, chartSubject } from './comments'
import { VoteControl } from '@/components/vote-control'
import { Modal, Notice } from './notifications'
import { useNotification } from './notification-context'
import { CategoryLabels } from './categories'
import { cn } from '@/lib/utils'
import { PageHero } from '@/components/page-hero'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { UploadPage } from './upload'
import { EmptyState } from '@/components/empty-state'
import { coverSource } from './cover'

export function Library({
  mine = false,
  ownerId,
  embedded = false,
}: {
  mine?: boolean
  /** Only this user's uploads, as on their user page. */
  ownerId?: string
  /** Inside another page: no page heading. */
  embedded?: boolean
}) {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams(),
    { user, loading: authLoading } = useSession()
  const [data, setData] = useState<ChartList | null>(null),
    [error, setError] = useState(''),
    [loading, setLoading] = useState(true)
  const [searchPending, setSearchPending] = useState(false)
  const search = useCallback(
    (query: string) => {
      setParams(
        (current) => {
          const next = new URLSearchParams(current)
          if (query) next.set('q', query)
          else next.delete('q')
          next.set('page', '1')
          return next
        },
        { replace: true },
      )
    },
    [setParams],
  )
  const q = params.get('q') || '',
    page = Math.min(10000, Math.max(1, Math.floor(Number(params.get('page')) || 1)))
  const level = Number(params.get('level'))
  const filters: ChartFilters = {
    order: chartOrders.includes(params.get('order') as ChartOrder)
      ? (params.get('order') as ChartOrder)
      : '',
    category: params.get('category') || '',
    course: (filterCourses as readonly string[]).includes(params.get('course') || '')
      ? params.get('course')!
      : '',
    level: Number.isInteger(level) && level >= 1 && level <= 10 ? String(level) : '',
    nijiiro: ['yes', 'no'].includes(params.get('nijiiro') || '') ? params.get('nijiiro')! : '',
    files: ['yes', 'no'].includes(params.get('files') || '') ? params.get('files')! : '',
  }
  const { order, category, course, nijiiro, files } = filters
  const stars = filters.level
  const uploader = mine ? '' : ownerId || ''

  useEffect(() => {
    if (mine && !user) return
    const controller = new AbortController()
    setLoading(true)
    setError('')
    api<ChartList>(
      endpoints.chartList(
        { q, page, order, category, course, level: stars, nijiiro, files, uploader },
        mine,
      ),
      { signal: controller.signal },
    )
      .then((result) => {
        if (!controller.signal.aborted) setData(result)
      })
      .catch((e) => {
        if (!controller.signal.aborted) setError(e.message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [mine, user, q, page, order, category, course, stars, nijiiro, files, uploader])
  if (mine && !user)
    return (
      <EmptyState title={t('db.myUploads')}>
        {authLoading ? (
          <p className="text-muted-foreground">{t('messages.loadingAccount')}</p>
        ) : (
          <Link className={buttonVariants({ variant: 'default', size: 'default' })} to="/login">
            {t('messages.signIn')}
            <ArrowRight size={16} />
          </Link>
        )}
      </EmptyState>
    )
  const update = (value: Record<string, string>) =>
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.set('page', '1')
      for (const [key, entry] of Object.entries(value)) {
        if (entry) next.set(key, entry)
        else next.delete(key)
      }
      return next
    })
  const filtered = !!(q || category || course || stars || nijiiro || files)
  return (
    <>
      {!embedded &&
        (mine ? (
          <h1 className="font-extrabold text-2xl sm:text-3xl">{t('db.myUploads')}</h1>
        ) : (
          <PageHero title={t('db.heroTitle')} />
        ))}
      <ChartSearch
        key={mine ? 'mine' : ownerId ? `user:${ownerId}` : 'all'}
        query={q}
        filters={filters}
        onSearch={search}
        onPendingChange={setSearchPending}
        onChange={update}
        counts={!mine && !uploader}
      />
      <h2 className="flex items-center gap-2 text-base font-extrabold [&_span]:text-muted-foreground">
        {q ? t('messages.searchResults', { query: q }) : t('db.results')}
        {!searchPending && !loading && !error && data && <span>{data.total}</span>}
      </h2>
      {error ? (
        <Notice>{error}</Notice>
      ) : loading && !data ? (
        <div
          className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3"
          aria-label={t('messages.loadingCharts')}
        >
          {[0, 1, 2].map((n) => (
            <Skeleton className="h-56 rounded-[14px]" key={n} />
          ))}
        </div>
      ) : data?.items.length ? (
        <>
          <div
            className={cn(
              'grid gap-4 sm:grid-cols-2 xl:grid-cols-3 transition-opacity',
              loading && 'opacity-60',
            )}
          >
            {data.items.map((chart) => (
              <ChartCard key={chart.id} chart={chart} />
            ))}
          </div>
          {data.total > data.pageSize && (
            <div className="flex flex-wrap items-center justify-center gap-4 py-4 text-sm">
              <Button
                variant="outline"
                size="default"
                disabled={page <= 1}
                onClick={() => update({ page: String(page - 1) })}
              >
                {t('messages.previous')}
              </Button>
              <span>
                {t('messages.pagination', {
                  page: page,
                  pages: Math.ceil(data.total / data.pageSize),
                })}
              </span>
              <Button
                variant="outline"
                size="default"
                disabled={page * data.pageSize >= data.total}
                onClick={() => update({ page: String(page + 1) })}
              >
                {t('messages.next')}
              </Button>
            </div>
          )}
        </>
      ) : (
        <EmptyState
          title={
            filtered
              ? t('messages.noMatchingCharts')
              : uploader
                ? t('messages.noPublishedChartsYet')
                : t('db.noCharts')
          }
        />
      )}
    </>
  )
}
export function Auth({ register = false }: { register?: boolean }) {
  const { t } = useTranslation()
  const [params] = useSearchParams()
  const session = useSession()
  const destination = params.get('returnTo') || '/'
  return (
    <div className="mx-auto max-w-lg space-y-4 py-8">
      <h1 className="text-2xl font-extrabold sm:text-3xl">
        {register ? t('messages.joinTheRhythm') : t('messages.welcomeBack')}
      </h1>
      <Card
        className="min-w-0 p-5 sm:p-6 gap-4 p-6 [&>p]:text-muted-foreground"
        aria-label={t('messages.accountCenterSignIn')}
      >
        <h2 className="text-base font-extrabold">
          {register
            ? t('messages.createAnAccountInTheAccountCenter')
            : t('messages.signInWithOurtaiko')}
        </h2>
        <p>{t('messages.signInNicknameAndPasswordAreManagedInTheAccountCenterYou')}</p>
        {params.has('error') && (
          <Notice>
            {t('messages.signInWasIncompleteOrAuthorizationExpiredPleaseSignInAgain')}
          </Notice>
        )}
        {session.error && <Notice>{session.error}</Notice>}
        <a
          className={buttonVariants({ variant: 'default', size: 'default', className: 'w-full' })}
          href={endpoints.login(destination)}
        >
          {t('messages.goToAccountCenter')}
          <ArrowRight size={17} />
        </a>
        <a
          className={buttonVariants({ variant: 'outline', size: 'default', className: 'w-full' })}
          href={endpoints.accountRegister}
          target="_blank"
          rel="noopener noreferrer"
        >
          {t('messages.createAnOurtaikoAccount')}
        </a>
      </Card>
    </div>
  )
}
const detailCardClassName = 'rounded-[14px] border-2 border-outline bg-card p-6 shadow-drop'
const manageItemClassName =
  'flex min-h-11 w-full items-center justify-between gap-3 text-left text-sm text-foreground transition-colors hover:text-link focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-md'
function formatBytes(bytes: number) {
  return bytes >= 1024 * 1024
    ? `${(bytes / 1024 / 1024).toFixed(1)} MiB`
    : `${Math.max(1, Math.round(bytes / 1024))} KiB`
}
type PageTab = 'info' | 'chart' | 'leaderboard' | 'comments'
type DetailSection = 'info' | 'activity' | 'comments'

export function Detail() {
  const { t } = useTranslation()
  const { notify } = useNotification()
  const { id, commentId } = useParams(),
    session = useSession(),
    navigate = useNavigate(),
    location = useLocation()
  const [chart, setChart] = useState<Chart | null>(null),
    [error, setError] = useState(''),
    [confirm, setConfirm] = useState(false),
    [busy, setBusy] = useState(false)
  const [editing, setEditing] = useState(false)
  const [coverEditing, setCoverEditing] = useState(false)
  const [collecting, setCollecting] = useState(false)
  // The block the chart area shows (block key); empty for the default one.
  const [chosenCourse, setChosenCourse] = useState('')
  // Below lg the page shows one section at a time; wider screens show them all.
  const [section, setSection] = useState<DetailSection>('info')
  const [activityTab, setActivityTab] = useState<ActivityTab>('image')
  const editButton = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    const controller = new AbortController()
    setChart(null)
    setError('')
    setEditing(false)
    setCoverEditing(false)
    setConfirm(false)
    setChosenCourse('')
    setActivityTab('image')
    if (!id) return
    api<Chart>(endpoints.chart(id), { signal: controller.signal })
      .then(setChart)
      .catch((e) => {
        if (!controller.signal.aborted) setError(e.message)
      })
    return () => controller.abort()
  }, [id])
  useEffect(() => {
    setSection(commentId || location.hash === '#comments' ? 'comments' : 'info')
  }, [id, commentId, location.hash])
  const remove = async () => {
    if (!chart) return
    setError('')
    setBusy(true)
    try {
      await api(endpoints.chart(chart.id), jsonRequest('DELETE', {}, session.csrfToken))
      setConfirm(false)
      navigate('/')
      notify(t('messages.chartDeleted'), 'success')
    } catch (e) {
      setConfirm(false)
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  if (!id) return <Notice>{t('messages.missingSongId')}</Notice>
  if (!chart)
    return error ? (
      <Notice>{error}</Notice>
    ) : (
      <p className="text-sm text-muted-foreground">{t('messages.loadingChart')}</p>
    )
  if (!supportsChart(chart)) return <Notice>{t('messages.thisChartTypeIsNotSupported')}</Notice>
  const course = chart.difficulties.some((d) => slotKey(d) === chosenCourse)
    ? chosenCourse
    : defaultDifficulty(chart.difficulties)
  const chosenDifficulty = chart.difficulties.find((d) => slotKey(d) === course)
  const showConstants = hasConstants(chart.difficulties)
  const pageTab: PageTab =
    section === 'activity' ? (activityTab === 'leaderboard' ? 'leaderboard' : 'chart') : section
  const selectPageTab = (value: PageTab) => {
    if (value === 'info' || value === 'comments') return setSection(value)
    setSection('activity')
    if (value === 'leaderboard') setActivityTab('leaderboard')
    else if (activityTab === 'leaderboard') setActivityTab('image')
  }
  // A chart without files has no leaderboard to open.
  const pageTabs: [PageTab, string][] = [
    ['info', t('messages.aboutTab')],
    ['chart', t('messages.chartTab')],
    ...(chart.hasFiles ? [['leaderboard', t('messages.leaderboardTab')] as [PageTab, string]] : []),
    ['comments', t('interactions.publicCommentCount')],
  ]
  const text = chartText(chart, i18n.resolvedLanguage)
  // The API decides; this only hides controls that would be refused.
  const canManage =
    !!session.user &&
    (session.user.isAdmin || (session.user.canUpload && session.user.id === chart.uploaderId))
  const date = (value: string) =>
    new Date(value).toLocaleString(formatLocale(i18n.resolvedLanguage), {
      dateStyle: 'medium',
      timeStyle: 'short',
    })
  return (
    <div className="flex flex-col gap-6">
      <section
        aria-labelledby="chart-title"
        className="overflow-hidden rounded-[14px] border-2 border-outline bg-card shadow-drop"
      >
        <div className="grid grid-cols-[7rem_minmax(0,1fr)] items-start gap-4 p-4 sm:grid-cols-[16rem_minmax(0,1fr)] sm:gap-x-8 sm:gap-y-5 sm:p-8 lg:grid-cols-[20rem_minmax(0,1fr)] lg:gap-x-10">
          <ChartCover chart={chart} className="sm:row-span-3" />
          <div className="flex min-w-0 flex-col gap-1 self-center sm:gap-2 sm:self-start">
            <CategoryLabels ids={chart.categoryIds} />
            <h1 id="chart-title" className="text-xl font-extrabold wrap-anywhere sm:text-4xl">
              {text.title}
            </h1>
            {text.subtitle && (
              <p className="truncate text-sm text-muted-foreground sm:whitespace-normal sm:text-lg sm:wrap-anywhere">
                {text.subtitle}
              </p>
            )}
            {chart.title !== text.title && (
              <p className="text-sm text-muted-foreground wrap-anywhere">{chart.title}</p>
            )}
          </div>
          <dl className="col-span-2 grid max-w-lg grid-cols-4 gap-2 rounded-xl bg-muted p-3 text-center sm:col-span-1 sm:col-start-2 sm:gap-4 sm:bg-transparent sm:p-0 sm:text-left sm:dark:bg-transparent">
            <div>
              <dt className="text-xs text-muted-foreground">{t('db.nijiiroId')}</dt>
              <dd
                data-testid="detail-nijiiro"
                className="text-base font-semibold tabular-nums sm:text-xl"
              >
                {chart.nijiiroId ?? '—'}
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('db.databaseId')}</dt>
              <dd className="text-base font-semibold tabular-nums sm:text-xl">{chart.id}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">BPM</dt>
              <dd className="text-base font-semibold tabular-nums sm:text-xl">
                {chart.bpm > 0 ? chart.bpm : '—'}
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('messages.duration')}</dt>
              <dd className="text-base font-semibold tabular-nums sm:text-xl">
                {chart.hasFiles ? formatDuration(chart.duration) : '—'}
              </dd>
            </div>
          </dl>
          <div className="col-span-2 flex flex-wrap items-center gap-2 sm:col-span-1 sm:col-start-2 sm:gap-3">
            {chart.hasFiles ? (
              <>
                <a
                  className={cn(buttonVariants({ size: 'lg' }), 'h-11 flex-1 px-5 sm:flex-none')}
                  href={endpoints.resource(chart, 'download')}
                >
                  <Download size={18} aria-hidden="true" />
                  {t('messages.downloadChartPackage')}
                </a>
                <a
                  className={cn(buttonVariants({ variant: 'secondary', size: 'lg' }), 'h-11')}
                  href={endpoints.resource(chart, 'tja')}
                >
                  <ArrowSquareOutIcon size={17} aria-hidden="true" />
                  TJA
                </a>
              </>
            ) : canManage ? (
              <Link
                className={cn(buttonVariants({ size: 'lg' }), 'h-11 flex-1 px-5 sm:flex-none')}
                to={`/charts/${chart.id}/update`}
              >
                <UploadSimpleIcon size={18} aria-hidden="true" />
                {t('db.uploadChartFiles')}
              </Link>
            ) : null}
            {session.user && (
              <Button
                variant="secondary"
                size="lg"
                className="h-11"
                aria-label={t('playlists.addToPlaylist')}
                onClick={() => setCollecting(true)}
              >
                <PlaylistIcon size={17} aria-hidden="true" />
                <span className="max-sm:hidden">{t('playlists.addToPlaylist')}</span>
              </Button>
            )}
            <div className="hidden flex-1 sm:block" />
            <VoteControl
              className="rounded-full bg-muted px-1 py-0.5"
              score={chart.score}
              myVote={chart.myVote}
              label={t('interactions.voteOnChart')}
              onVote={(value: Vote) =>
                api<VoteResult>(
                  endpoints.chartVote(chart.id),
                  jsonRequest('PUT', { value }, session.csrfToken),
                )
              }
              onResult={(result) =>
                setChart((current) =>
                  current?.id === chart.id ? { ...current, ...result } : current,
                )
              }
            />
            <a
              href="#comments"
              onClick={() => setSection('comments')}
              className="inline-flex h-11 items-center gap-1.5 rounded-full px-3 text-sm text-muted-foreground hover:text-foreground max-sm:hidden"
            >
              <ChatCircleIcon size={18} aria-hidden="true" />
              {t('interactions.commentCount', { count: chart.commentCount })}
            </a>
          </div>
        </div>
        {chart.hasFiles ? (
          <AudioPlayer
            compact
            className="rounded-none border-0 border-t bg-muted/60 px-4 py-3 sm:px-8"
            label={t('messages.audioPreview')}
            src={endpoints.resource(chart, 'audio')}
            startAt={chart.demoStart}
            knownDuration={chart.duration}
          />
        ) : (
          <p
            role="status"
            className="flex items-center gap-2 border-t bg-muted/60 px-4 py-3 text-sm text-muted-foreground sm:px-8"
          >
            <FileDashedIcon size={18} aria-hidden="true" />
            {t('db.noFilesText')}
          </p>
        )}
      </section>
      {error && <Notice>{error}</Notice>}
      {collecting && session.user && (
        <AddToPlaylistDialog chart={chart} onDismiss={() => setCollecting(false)} />
      )}
      {editing && canManage && (
        <EditMetadata
          key={chart.id}
          chart={chart}
          csrf={session.csrfToken}
          onDismiss={() => setEditing(false)}
          onSaved={(updated) => {
            setChart(updated)
            setEditing(false)
            notify(t('messages.chartInformationSaved'), 'success')
          }}
        />
      )}
      {coverEditing && canManage && (
        <CoverDialog
          chart={chart}
          onDismiss={() => setCoverEditing(false)}
          onSaved={(coverHash) =>
            setChart((current) => (current?.id === chart.id ? { ...current, coverHash } : current))
          }
        />
      )}
      <Tabs
        value={pageTab}
        onValueChange={(value) => selectPageTab(value as PageTab)}
        className="lg:hidden"
      >
        {/* Short labels, each tab as wide as its label needs; one that still does not fit is cut short. */}
        <TabsList
          aria-label={t('messages.chartSections')}
          className="flex h-auto w-full gap-0.5 rounded-[12px] bg-muted p-1"
        >
          {pageTabs.map(([value, label]) => (
            <TabsTrigger
              key={value}
              value={value}
              className="h-9 min-w-0 flex-auto px-1.5 text-sm text-muted-foreground after:hidden"
            >
              <span className="truncate">{label}</span>
              {value === 'comments' && chart.commentCount > 0 && (
                <span className="text-xs tabular-nums">{chart.commentCount}</span>
              )}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <div
          className={cn(
            'flex min-w-0 flex-col gap-6 lg:col-start-1 lg:row-start-1',
            section === 'info' && 'max-lg:hidden',
          )}
        >
          <div
            className={cn('flex min-w-0 flex-col gap-6', section !== 'activity' && 'max-lg:hidden')}
          >
            {/* Below lg the constants head the chart section; from lg, the sidebar. */}
            {showConstants && (
              <DifficultyConstants
                key={course}
                difficulty={chosenDifficulty}
                className="lg:hidden"
              />
            )}
            {chart.hasFiles ? (
              <ChartActivity
                key={`${chart.id}:${chart.difficulties.map((d) => d.chartHash ?? '').join(',')}`}
                chart={chart}
                tab={activityTab}
                onTabChange={setActivityTab}
                course={course}
                onCourseChange={setChosenCourse}
              />
            ) : (
              <section
                className={cn(
                  detailCardClassName,
                  'flex flex-col items-center gap-3 py-12 text-center',
                )}
                aria-label={t('messages.chartImage')}
              >
                <FileDashedIcon size={32} className="text-muted-foreground" aria-hidden="true" />
                <h2 className="text-lg font-extrabold">{t('db.noFiles')}</h2>
                <p className="max-w-md text-sm text-muted-foreground">
                  {canManage ? t('db.noFilesManagerText') : t('db.noFilesText')}
                </p>
                {canManage && (
                  <Link
                    className={buttonVariants({ variant: 'outline' })}
                    to={`/charts/${chart.id}/update`}
                  >
                    <UploadSimpleIcon size={16} aria-hidden="true" />
                    {t('db.uploadChartFiles')}
                  </Link>
                )}
              </section>
            )}
          </div>
          <div className={cn('min-w-0', section !== 'comments' && 'max-lg:hidden')}>
            <CommentSection
              subject={chartSubject(chart)}
              focusId={commentId}
              onCountChange={(delta) =>
                setChart((current) =>
                  current?.id === chart.id
                    ? { ...current, commentCount: Math.max(0, current.commentCount + delta) }
                    : current,
                )
              }
            />
          </div>
        </div>
        <aside
          className={cn(
            'flex min-w-0 flex-col gap-6 lg:col-start-2 lg:row-start-1',
            section !== 'info' && 'max-lg:hidden',
          )}
        >
          {showConstants && (
            <DifficultyConstants
              key={course}
              difficulty={chosenDifficulty}
              className="max-lg:hidden"
            />
          )}
          <section
            aria-labelledby="chart-record-heading"
            className={cn(detailCardClassName, 'space-y-4')}
          >
            <h2 id="chart-record-heading" className="text-lg font-extrabold">
              {t('db.record')}
            </h2>
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-3 text-sm [&>dt]:text-muted-foreground [&>dd]:wrap-anywhere">
              <dt>{t('db.nijiiroId')}</dt>
              <dd className="tabular-nums">{chart.nijiiroId ?? t('db.none')}</dd>
              <dt>{t('db.officialId')}</dt>
              <dd>{chart.officialId ?? t('db.none')}</dd>
              <dt>{t('db.databaseId')}</dt>
              <dd className="tabular-nums">{chart.id}</dd>
              <dt className="self-center">{t('db.uploader')}</dt>
              <dd className="inline-flex items-center gap-2">
                <UserAvatar
                  nickname={chart.uploader}
                  avatarUrl={chart.uploaderAvatarUrl}
                  className="size-6"
                  fallbackClassName="text-xs"
                />
                <span className="min-w-0 wrap-anywhere">{chart.uploader}</span>
              </dd>
              <dt>{t('db.created')}</dt>
              <dd>
                <time dateTime={chart.createdAt}>{date(chart.createdAt)}</time>
              </dd>
              <dt>{t('db.updated')}</dt>
              <dd>
                <time dateTime={chart.updateAt}>{date(chart.updateAt)}</time>
              </dd>
            </dl>
          </section>
          <section
            aria-labelledby="chart-files-heading"
            className={cn(detailCardClassName, 'space-y-4')}
          >
            <h2 id="chart-files-heading" className="text-lg font-extrabold">
              {t('db.files')}
            </h2>
            {!chart.hasFiles && <p className="text-sm text-muted-foreground">{t('db.noFiles')}</p>}
            <dl className="space-y-3 text-sm [&_dt]:text-muted-foreground">
              {(chart.hasFiles
                ? ([
                    // The TJA's own hash is not published any more: its blocks' chartHash are the
                    // chart's identity, and the download checks the resource manifest's sha256.
                    ['TJA', chart.tjaName, '', chart.tjaSize, 'tja'],
                    [t('db.audio'), chart.audioName, chart.audioHash, chart.audioSize, 'audio'],
                  ] as const)
                : []
              ).map(([label, name, hash, size, kind]) => (
                <div key={kind} className="space-y-1">
                  <dt>
                    {label} · {formatBytes(size)}
                  </dt>
                  <dd className="wrap-anywhere">
                    <a className="text-link hover:underline" href={endpoints.resource(chart, kind)}>
                      {name}
                    </a>
                  </dd>
                  {hash && (
                    <dd
                      className="font-mono text-xs break-all text-muted-foreground"
                      title="SHA-256"
                    >
                      {hash}
                    </dd>
                  )}
                </div>
              ))}
            </dl>
          </section>
          {chart.description && (
            <section
              aria-labelledby="chart-description-heading"
              className={cn(detailCardClassName, 'space-y-3')}
            >
              <h2 id="chart-description-heading" className="text-lg font-extrabold">
                {t('db.notes')}
              </h2>
              <p className="whitespace-pre-wrap text-sm leading-7 text-muted-foreground wrap-anywhere">
                {chart.description}
              </p>
            </section>
          )}
          {canManage && (
            <section
              aria-labelledby="chart-manage-heading"
              className={cn(detailCardClassName, 'space-y-1')}
            >
              <h2 id="chart-manage-heading" className="pb-2 text-lg font-extrabold">
                {t('messages.manageChart')}
              </h2>
              <ul className="divide-y">
                <li>
                  <Link className={manageItemClassName} to={`/charts/${chart.id}/update`}>
                    {chart.hasFiles ? t('messages.updateSongAndCharts') : t('db.uploadChartFiles')}
                    <CaretRightIcon
                      className="size-4 shrink-0 text-muted-foreground"
                      aria-hidden="true"
                    />
                  </Link>
                </li>
                {chart.hasFiles && (
                  <li>
                    <Link className={manageItemClassName} to={`/charts/${chart.id}/edit`}>
                      {t('editor.open')}
                      <CaretRightIcon
                        className="size-4 shrink-0 text-muted-foreground"
                        aria-hidden="true"
                      />
                    </Link>
                  </li>
                )}
                <li>
                  <button
                    ref={editButton}
                    type="button"
                    className={manageItemClassName}
                    onClick={() => setEditing(true)}
                  >
                    {t('messages.editInformation')}
                    <CaretRightIcon
                      className="size-4 shrink-0 text-muted-foreground"
                      aria-hidden="true"
                    />
                  </button>
                </li>
                <li>
                  <button
                    type="button"
                    className={manageItemClassName}
                    onClick={() => setCoverEditing(true)}
                  >
                    {coverSource(chart) ? t('messages.changeCover') : t('messages.addCover')}
                    <CaretRightIcon
                      className="size-4 shrink-0 text-muted-foreground"
                      aria-hidden="true"
                    />
                  </button>
                </li>
                <li>
                  <button
                    type="button"
                    className={cn(manageItemClassName, 'text-destructive hover:text-destructive')}
                    onClick={() => setConfirm(true)}
                  >
                    {t('messages.deleteChart')}
                  </button>
                </li>
              </ul>
              {confirm && (
                <Modal
                  title={t('messages.deleteThisChart')}
                  busy={busy}
                  onDismiss={() => setConfirm(false)}
                  actions={
                    <>
                      <DialogClose
                        render={<Button type="button" variant="outline" size="default" />}
                        disabled={busy}
                      >
                        {t('messages.cancel')}
                      </DialogClose>
                      <Button
                        type="button"
                        variant="destructive"
                        size="default"
                        disabled={busy}
                        onClick={remove}
                      >
                        {busy ? t('messages.deleting') : t('messages.confirmDeletion')}
                      </Button>
                    </>
                  }
                >
                  {t('db.deleteText')}
                </Modal>
              )}
            </section>
          )}
        </aside>
      </div>
    </div>
  )
}

// My uploads and the upload form on one page: /me/charts lists the user's
// uploads, /me/charts/publish uploads a new chart.
export function ChartManagerPage() {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const tab = pathname.replace(/\/$/, '').endsWith('/publish') ? 'publish' : 'charts'
  return (
    <>
      <h1 className="font-extrabold text-2xl sm:text-3xl">{t('messages.chartManager')}</h1>
      <Tabs
        value={tab}
        onValueChange={(value) =>
          navigate(value === 'publish' ? '/me/charts/publish' : '/me/charts')
        }
      >
        <TabsList aria-label={t('messages.chartManager')}>
          <TabsTrigger value="charts">{t('db.myUploads')}</TabsTrigger>
          <TabsTrigger value="publish">
            <UploadSimpleIcon aria-hidden="true" />
            {t('db.upload')}
          </TabsTrigger>
        </TabsList>
      </Tabs>
      {tab === 'publish' ? <UploadPage embedded /> : <Library mine embedded />}
    </>
  )
}
