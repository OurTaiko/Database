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
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  ArrowRightIcon as ArrowRight,
  DownloadSimpleIcon as Download,
  ArrowSquareOutIcon,
  CaretRightIcon,
} from '@phosphor-icons/react'
import { api, jsonRequest } from './api/client'
import type { Chart, ChartList } from './api/types'
import { ChartCard, formatDuration } from '@/components/chart-card'
import { supportsChart } from './courses'
import { useSession } from './session-context'
import { EditMetadata } from './edit-metadata'
import { ChartActivity } from './chart-activity'
import { Modal, Notice } from './notifications'
import { useNotification } from './notification-context'
import { CategoryLabels, useCategories } from './categories'
import { cn } from '@/lib/utils'
import { coverSource } from './cover'

const emptyStateClassName =
  'flex flex-col items-center justify-center gap-4 rounded-3xl border border-dashed bg-card/60 px-6 py-16 text-center [&>p]:max-w-lg [&>p]:text-muted-foreground'

export function Library({ mine = false }: { mine?: boolean }) {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams(),
    { user, loading: authLoading } = useSession()
  const { chartCount } = useCategories()
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
  }
  const { order, category, course, nijiiro } = filters
  const stars = filters.level
  useEffect(() => {
    if (mine && !user) return
    const controller = new AbortController()
    setLoading(true)
    setError('')
    api<ChartList>(
      endpoints.chartList({ q, page, order, category, course, level: stars, nijiiro }, mine),
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
  }, [mine, user, q, page, order, category, course, stars, nijiiro])
  if (mine && !user)
    return (
      <div className={emptyStateClassName}>
        <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">{t('db.myUploads')}</h1>
        <p>{authLoading ? t('messages.loadingAccount') : t('db.signInToSeeUploads')}</p>
        {!authLoading && (
          <Link className={buttonVariants({ variant: 'default', size: 'default' })} to="/login">
            {t('messages.signIn')}
            <ArrowRight size={16} />
          </Link>
        )}
      </div>
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
  const filtered = !!(q || category || course || stars || nijiiro)
  return (
    <>
      <div className="[&_p]:mt-2 [&_p]:text-sm [&_p]:text-muted-foreground">
        <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">
          {mine ? t('db.myUploads') : t('db.heroTitle')}
        </h1>
        <p>{mine ? t('db.myUploadsText') : t('db.heroText', { count: chartCount })}</p>
      </div>
      <ChartSearch
        key={mine ? 'mine' : 'all'}
        query={q}
        filters={filters}
        onSearch={search}
        onPendingChange={setSearchPending}
        onChange={update}
      />
      <h2 className="flex items-center gap-2 text-base font-semibold [&_span]:text-muted-foreground">
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
            <Skeleton className="h-56 rounded-2xl" key={n} />
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
        <div className={emptyStateClassName}>
          <h2 className="text-base font-semibold">
            {filtered ? t('messages.noMatchingCharts') : t('db.noCharts')}
          </h2>
          <p>{filtered ? t('messages.tryOtherSearchFilters') : t('db.noChartsText')}</p>
        </div>
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
      <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">
        {register ? t('messages.joinTheRhythm') : t('messages.welcomeBack')}
      </h1>
      <p className="text-sm text-muted-foreground">{t('db.accountText')}</p>
      <Card
        className="min-w-0 border p-5 shadow-none ring-0 sm:p-6 gap-4 p-6 [&>p]:text-muted-foreground"
        aria-label={t('messages.accountCenterSignIn')}
      >
        <h2 className="text-base font-semibold">
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
        <p className="text-sm text-muted-foreground">
          {t('messages.registrationOpensInANewTabReturnHereToSignInWhen')}
        </p>
      </Card>
    </div>
  )
}
const detailCardClassName =
  'rounded-2xl bg-white p-6 shadow-[0_4px_12px_rgba(0,0,0,0.08)] dark:bg-card'
const manageItemClassName =
  'flex min-h-11 w-full items-center justify-between gap-3 text-left text-sm text-foreground transition-colors hover:text-[#0071e3] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-md'
function formatBytes(bytes: number) {
  return bytes >= 1024 * 1024
    ? `${(bytes / 1024 / 1024).toFixed(1)} MiB`
    : `${Math.max(1, Math.round(bytes / 1024))} KiB`
}
export function Detail() {
  const { t } = useTranslation()
  const { notify } = useNotification()
  const { id } = useParams(),
    session = useSession(),
    navigate = useNavigate()
  const [chart, setChart] = useState<Chart | null>(null),
    [error, setError] = useState(''),
    [confirm, setConfirm] = useState(false),
    [busy, setBusy] = useState(false)
  const [editing, setEditing] = useState(false)
  const [coverEditing, setCoverEditing] = useState(false)
  const editButton = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    const controller = new AbortController()
    setChart(null)
    setError('')
    setEditing(false)
    setCoverEditing(false)
    setConfirm(false)
    if (!id) return
    api<Chart>(endpoints.chart(id), { signal: controller.signal })
      .then(setChart)
      .catch((e) => {
        if (!controller.signal.aborted) setError(e.message)
      })
    return () => controller.abort()
  }, [id])
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
  if (!supportsChart(chart.difficulties))
    return <Notice>{t('messages.thisChartTypeIsNotSupported')}</Notice>
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
        className="overflow-hidden rounded-2xl bg-white shadow-[0_4px_12px_rgba(0,0,0,0.08)] dark:bg-card"
      >
        <div className="grid grid-cols-[7rem_minmax(0,1fr)] items-start gap-4 p-4 sm:grid-cols-[16rem_minmax(0,1fr)] sm:gap-x-8 sm:gap-y-5 sm:p-8 lg:grid-cols-[20rem_minmax(0,1fr)] lg:gap-x-10">
          <ChartCover chart={chart} className="sm:row-span-3" />
          <div className="flex min-w-0 flex-col gap-1 self-center sm:gap-2 sm:self-start">
            <CategoryLabels ids={chart.categoryIds} />
            <h1
              id="chart-title"
              className="text-xl font-semibold tracking-tight wrap-anywhere sm:text-4xl"
            >
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
          <dl className="col-span-2 grid max-w-lg grid-cols-4 gap-2 rounded-xl bg-[#f5f5f7] p-3 text-center sm:col-span-1 sm:col-start-2 sm:gap-4 sm:bg-transparent sm:p-0 sm:text-left dark:bg-muted sm:dark:bg-transparent">
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
              <dd className="text-base font-semibold tabular-nums sm:text-xl">{chart.bpm}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('messages.duration')}</dt>
              <dd className="text-base font-semibold tabular-nums sm:text-xl">
                {formatDuration(chart.duration)}
              </dd>
            </div>
          </dl>
          <div className="col-span-2 flex flex-wrap items-center gap-2 sm:col-span-1 sm:col-start-2 sm:gap-3">
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
          </div>
        </div>
        <AudioPlayer
          compact
          className="rounded-none border-0 border-t bg-[#fbfbfd] px-4 py-3 sm:px-8 dark:bg-muted/40"
          label={t('messages.audioPreview')}
          src={endpoints.resource(chart, 'audio')}
          startAt={chart.demoStart}
          knownDuration={chart.duration}
        />
      </section>
      {error && <Notice>{error}</Notice>}
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
      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <div className="min-w-0">
          <ChartActivity key={`${chart.id}:${chart.tjaHash}`} chart={chart} />
        </div>
        <aside className="flex min-w-0 flex-col gap-6">
          <section
            aria-labelledby="chart-record-heading"
            className={cn(detailCardClassName, 'space-y-4')}
          >
            <h2 id="chart-record-heading" className="text-lg font-semibold tracking-tight">
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
            <h2 id="chart-files-heading" className="text-lg font-semibold tracking-tight">
              {t('db.files')}
            </h2>
            <dl className="space-y-3 text-sm [&_dt]:text-muted-foreground">
              {(
                [
                  ['TJA', chart.tjaName, chart.tjaHash, chart.tjaSize, 'tja'],
                  [t('db.audio'), chart.audioName, chart.audioHash, chart.audioSize, 'audio'],
                ] as const
              ).map(([label, name, hash, size, kind]) => (
                <div key={kind} className="space-y-1">
                  <dt>
                    {label} · {formatBytes(size)}
                  </dt>
                  <dd className="wrap-anywhere">
                    <a
                      className="text-primary hover:underline"
                      href={endpoints.resource(chart, kind)}
                    >
                      {name}
                    </a>
                  </dd>
                  <dd className="font-mono text-xs break-all text-muted-foreground" title="SHA-256">
                    {hash}
                  </dd>
                </div>
              ))}
            </dl>
          </section>
          {chart.description && (
            <section
              aria-labelledby="chart-description-heading"
              className={cn(detailCardClassName, 'space-y-3')}
            >
              <h2 id="chart-description-heading" className="text-lg font-semibold tracking-tight">
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
              <h2 id="chart-manage-heading" className="pb-2 text-lg font-semibold tracking-tight">
                {t('messages.manageChart')}
              </h2>
              <ul className="divide-y">
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
