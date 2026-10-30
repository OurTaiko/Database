import { isChartOwner } from '@sitekit/chart-owners'
import { useTranslation } from 'react-i18next'
import { chartText } from '@sitekit/chart-language'
import { i18n } from '@sitekit/i18n'
import { endpoints } from '@sitekit/api/endpoints'
import { Card } from '@sitekit/components/ui/card'
import { Label } from '@sitekit/components/ui/label'
import { Input } from '@sitekit/components/ui/input'
import { Button, buttonVariants } from '@sitekit/components/ui/button'
import { Textarea } from '@sitekit/components/ui/textarea'
import { Progress } from '@sitekit/components/ui/progress'
import { useEffect, useRef, useState } from 'react'
import type { DragEvent, FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  WaveformIcon as AudioLines,
  FileAudioIcon as FileMusic,
  CheckIcon as Check,
  CircleNotchIcon as LoaderCircle,
  UploadSimpleIcon as Upload,
} from '@phosphor-icons/react'
import { CoverPicker } from '@sitekit/components/cover-picker'
import { MoviePicker } from '@sitekit/components/movie-picker'
import { AudioPlayer } from '@sitekit/components/audio-player'
import { Tabs, TabsList, TabsTrigger } from '@sitekit/components/ui/tabs'
import { CategoryPicker } from '@sitekit/categories'
import { api, previewReplacement, uploadChart } from '@sitekit/api/client'
import type { Chart } from './api/types'
import { EmptyState } from '@sitekit/components/empty-state'
import { Notice } from '@sitekit/notifications'
import { useNotification } from '@sitekit/notification-context'
import { useSession } from '@sitekit/session-context'
import {
  isAudioFilename,
  isMovieFilename,
  prepareTja,
  validateFiles,
  validateMovie,
} from '@sitekit/tja'
import type { PreparedTja } from '@sitekit/tja'
import { blockName, slotKey } from '@sitekit/courses'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@sitekit/components/ui/table'
import { officialIdPattern, parseBpm, parseNijiiroId } from './edit-metadata'
import { audioDiffers, replacementFields, reviewReplacement } from '@sitekit/replacement-diff'
import type { Proposal, ReplacementPreview, Review } from '@sitekit/replacement-diff'
import { ReplacementReview } from '@sitekit/replacement-review'

function createRequestKey() {
  // getRandomValues remains available over LAN HTTP, unlike randomUUID.
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}
type UploadMode = 'files' | 'song'

/**
 * Replaces a chart's files through the upload page, as on Fanmade: the page
 * at /charts/:id/update loads the chart and renders UploadPage with it. A
 * song registered without files (R038) gets its first TJA and audio here too.
 */
export function UpdatePage() {
  const { t } = useTranslation()
  const { id } = useParams()
  const session = useSession()
  const [chart, setChart] = useState<Chart | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    const controller = new AbortController()
    setChart(null)
    setError('')
    if (!id) return
    api<Chart>(endpoints.chartDetail(id), { signal: controller.signal })
      .then((loaded) => {
        if (!controller.signal.aborted) setChart(loaded)
      })
      .catch((e: Error) => {
        if (!controller.signal.aborted) setError(e.message)
      })
    return () => controller.abort()
  }, [id])
  if (!id) return <Notice>{t('messages.missingSongId')}</Notice>
  if (error) return <Notice>{error}</Notice>
  if (!chart || session.loading) return <p role="status">{t('messages.loadingSong')}</p>
  // The API decides; this only hides a form that would be refused.
  const canManage = !!session.user && (session.user.isAdmin || isChartOwner(chart, session.user.id))
  if (!canManage) return <p>{t('messages.onlyTheUploaderOrAnAdministratorCanUpdateThisSong')}</p>
  return (
    <UploadPage
      key={`${chart.id}:${chart.difficulties.map((d) => d.chartHash ?? '').join(',')}:${chart.audioHash}`}
      existing={chart}
    />
  )
}

// Adds an official chart: a TJA, its audio and what identifies the song. The
// API parses and validates the files; this page only checks what it can see.
// In song mode no files are sent: a song that is known but whose files are
// not at hand is registered by its title, and its files are uploaded later
// from its page. With `existing` the same form replaces that chart's files
// through PUT /charts/{id}/files: a new TJA is required, the audio is kept
// unless a new one is chosen, and the text fields start from the chart's
// values. A chart that has files first shows what the replacement changes
// (the server reads the new TJA), and its names can be kept.
export function UploadPage({
  existing,
  embedded = false,
}: {
  existing?: Chart
  embedded?: boolean
}) {
  const { t } = useTranslation()
  const { notify } = useNotification()
  const session = useSession(),
    navigate = useNavigate()
  const [mode, setMode] = useState<UploadMode>('files')
  const [tja, setTja] = useState<File | null>(null),
    [audio, setAudio] = useState<File | null>(null),
    [cover, setCover] = useState<File | null>(null)
  const [title, setTitle] = useState(''),
    [subtitle, setSubtitle] = useState(''),
    [bpm, setBpm] = useState('')
  const [nijiiroId, setNijiiroId] = useState(
      existing?.nijiiroId != null ? String(existing.nijiiroId) : '',
    ),
    [officialId, setOfficialId] = useState(existing?.officialId ?? '')
  const [categoryIds, setCategoryIds] = useState<string[]>(existing?.categoryIds ?? [])
  const [description, setDescription] = useState(existing?.description ?? ''),
    [busy, setBusy] = useState(false),
    [progress, setProgress] = useState(0)
  // A replacement's review: open while the new file is read (review null) and until confirmed.
  const [reviewing, setReviewing] = useState<{
    preview: ReplacementPreview | null
    review: Review | null
    audioChanged: boolean
  } | null>(null)
  const attemptedKeep = useRef('')
  // The audio of a chart that has files stays unless a new one is chosen.
  const replacing = !!existing?.hasFiles
  const retainedAudioName = replacing ? existing!.audioName : undefined
  const requestKey = useRef(createRequestKey()),
    controller = useRef<AbortController | null>(null)
  const [movie, setMovie] = useState<File | null>(null)
  const hasStoredMovie = replacing && !!existing?.movieHash
  // The preview range starts as the new TJA's DEMOSTART for 15 seconds.
  const [demoStart, setDemoStart] = useState('0'),
    [demoEnd, setDemoEnd] = useState('15')
  // The chosen files as the browser checks them (the TJA converted to UTF-8, which the site
  // takes only), before the server reads them again.
  const [metadata, setMetadata] = useState<PreparedTja | null>(null),
    [validating, setValidating] = useState(false),
    [validationError, setValidationError] = useState('')
  // Makers typed in the makers table, by slot; the others are the file's.
  const [typedMakers, setTypedMakers] = useState<Record<string, string>>({})
  const [audioUrl, setAudioUrl] = useState('')
  const [audioDuration, setAudioDuration] = useState<number | null>(null)
  useEffect(() => () => controller.current?.abort(), [])
  useEffect(() => {
    let active = true
    setMetadata(null)
    setValidationError('')
    if (!tja || (!audio && !retainedAudioName)) {
      setValidating(false)
      return
    }
    setValidating(true)
    const validation = audio ? validateFiles(tja, audio) : prepareTja(tja, '', retainedAudioName)
    validation
      .then((m) => {
        validateMovie(m, movie, hasStoredMovie)
        if (!active) return
        setMetadata(m)
        setTypedMakers({})
        setDemoStart(String(m.demoStart))
        setDemoEnd(String(m.demoStart + 15))
      })
      .catch((e: Error) => {
        if (active) setValidationError(e.message)
      })
      .finally(() => {
        if (active) setValidating(false)
      })
    return () => {
      active = false
    }
  }, [tja, audio, movie, retainedAudioName, hasStoredMovie])
  useEffect(() => {
    setAudioUrl('')
    setAudioDuration(null)
    if (!audio) return
    const url = URL.createObjectURL(audio)
    setAudioUrl(url)
    const player = new Audio(url)
    player.preload = 'metadata'
    player.onloadedmetadata = () => {
      if (Number.isFinite(player.duration)) setAudioDuration(player.duration)
    }
    return () => {
      player.onloadedmetadata = null
      player.removeAttribute('src')
      URL.revokeObjectURL(url)
    }
  }, [audio])
  if (session.loading) return <p role="status">{t('messages.loadingAccount')}</p>
  // Only administrators upload; the API refuses anyone else. The route asks visitors to sign in.
  if (!session.user?.isAdmin)
    return (
      <EmptyState title={t('db.uploadUnavailable')}>
        <p className="text-muted-foreground text-sm">{t('db.uploadNeedsPermission')}</p>
      </EmptyState>
    )
  const choose = (kind: 'tja' | 'audio', file: File | null) => {
    requestKey.current = createRequestKey()
    if (kind === 'tja') setTja(file)
    else setAudio(file)
  }
  const drop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault()
    if (busy) return
    const files = Array.from(e.dataTransfer.files),
      charts = files.filter((f) => /\.tja$/i.test(f.name)),
      tracks = files.filter((f) => isAudioFilename(f.name)),
      movies = files.filter((f) => isMovieFilename(f.name))
    if (
      charts.length > 1 ||
      tracks.length > 1 ||
      movies.length > 1 ||
      charts.length + tracks.length + movies.length !== files.length
    ) {
      notify(t('messages.dropOneTjaAndOneOggOrMp3NoAdditionalFiles'), 'error')
      return
    }
    if (charts[0]) choose('tja', charts[0])
    if (tracks[0]) choose('audio', tracks[0])
    if (movies[0]) {
      requestKey.current = createRequestKey()
      setMovie(movies[0])
    }
  }
  const fileError = mode === 'song' ? '' : validationError
  const ready = mode === 'song' ? !!title.trim() : !!metadata && !validating
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!ready || busy || fileError) return
    const official = officialId.trim()
    if (
      parseNijiiroId(nijiiroId) === undefined ||
      (official && !officialIdPattern.test(official))
    ) {
      notify(t('db.identifierInvalid'), 'error')
      return
    }
    if (mode === 'song' && parseBpm(bpm) === undefined) {
      notify(t('db.songFieldsInvalid'), 'error')
      return
    }
    if (mode === 'files' && !validPreview()) {
      notify(t('messages.previewRangeInvalid'), 'error')
      return
    }
    if (replacing) await startReview()
    else await save()
  }
  const validPreview = () => {
    const start = Number(demoStart),
      end = Number(demoEnd)
    const duration = audio ? audioDuration : retainedAudioName ? existing!.duration : null
    return (
      !!demoStart.trim() &&
      !!demoEnd.trim() &&
      Number.isFinite(start) &&
      Number.isFinite(end) &&
      start >= 0 &&
      (duration == null || start < duration) &&
      end > start &&
      end <= 1215
    )
  }
  // The range was typed rather than left at the new file's DEMOSTART.
  const typedDemo =
    !metadata ||
    demoStart !== String(metadata.demoStart) ||
    demoEnd !== String(metadata.demoStart + 15)
  const proposal = (): Proposal => ({
    description,
    categoryIds,
    nijiiroId: nijiiroId.trim(),
    officialId: officialId.trim(),
    demoStart: Number(demoStart),
    demoEnd: Number(demoEnd),
    typedMakers,
    typedDemo,
  })
  const startReview = async () => {
    const abort = new AbortController()
    controller.current = abort
    setReviewing({ preview: null, review: null, audioChanged: false })
    try {
      const [preview, audioChanged] = await Promise.all([
        previewReplacement(
          existing!.id,
          metadata!.file,
          audio?.name,
          session.csrfToken,
          abort.signal,
        ),
        audioDiffers(existing!, audio),
      ])
      if (abort.signal.aborted) return
      setReviewing({
        preview,
        audioChanged,
        review: reviewReplacement(existing!, preview, proposal(), audioChanged, {
          demo: true,
          makers: true,
        }),
      })
    } catch (e) {
      setReviewing(null)
      if (!abort.signal.aborted) notify((e as Error).message, 'error')
    }
  }
  const save = async (kept = new Set<string>()) => {
    const preview = reviewing?.preview
    setReviewing(null)
    const fields =
      existing && preview
        ? replacementFields(existing, preview, proposal(), kept)
        : {
            keepTitles: false,
            description,
            categoryIds,
            nijiiroId: nijiiroId.trim(),
            officialId: officialId.trim(),
            demoStart: Number(demoStart),
            demoEnd: Number(demoEnd),
            difficultyMakers: (metadata?.difficulties ?? []).map(({ course, side, maker }) => ({
              course,
              side,
              maker,
            })),
          }
    // Other choices are a different request; the same choices retry the last one.
    const choices = [...kept].sort().join(',')
    if (choices !== attemptedKeep.current) requestKey.current = createRequestKey()
    attemptedKeep.current = choices
    setBusy(true)
    setProgress(0)
    const abort = new AbortController()
    controller.current = abort
    try {
      const form = new FormData()
      if (mode === 'song') {
        form.append('title', title.trim())
        form.append('subtitle', subtitle.trim())
        form.append('bpm', bpm.trim())
      } else {
        form.append('tja', metadata!.file)
        if (audio) form.append('audio', audio)
        if (movie) form.append('movie', movie)
      }
      if (cover) form.append('cover', cover)
      if (replacing) form.append('confirmReset', 'true')
      if (fields.keepTitles) form.append('keepTitles', 'true')
      form.append('description', fields.description)
      form.append('categoryIds', JSON.stringify(fields.categoryIds))
      form.append('nijiiroId', fields.nijiiroId)
      form.append('officialId', fields.officialId)
      if (mode === 'files') form.append('difficultyMakers', JSON.stringify(fields.difficultyMakers))
      // A typed or kept range is sent; otherwise the server takes the new file's DEMOSTART.
      if (mode === 'files' && (typedDemo || kept.has('demo'))) {
        form.append('demoStart', String(fields.demoStart))
        form.append('demoEnd', String(fields.demoEnd))
      }
      const chart = await uploadChart<Chart>(
        form,
        session.csrfToken,
        requestKey.current,
        abort.signal,
        setProgress,
        existing?.id,
      )
      if ('status' in chart) {
        navigate('/me/charts')
        notify(t('messages.uploadAcceptedResultByNotification'), 'success')
        return
      }
      navigate(`/charts/${chart.id}`)
      notify(
        existing
          ? t(
              replacing
                ? 'messages.songUpdatedAllPreviousChartsAndScoresHaveBeenDeleted'
                : 'messages.chartFilesUploaded',
            )
          : mode === 'song'
            ? t('messages.songRegistered')
            : t('messages.chartPublished'),
        'success',
      )
    } catch (e) {
      if (!abort.signal.aborted) notify((e as Error).message, 'error')
    } finally {
      setBusy(false)
      controller.current = null
    }
  }
  const fileTile = (kind: 'tja' | 'audio') => {
    const file = kind === 'tja' ? tja : audio
    return (
      <Label className="flex min-w-0 flex-col items-start gap-3 rounded-[14px] border p-4 [&>span]:w-full [&_small]:mt-1 [&_small]:block [&_small]:truncate [&_small]:text-muted-foreground">
        {kind === 'tja' ? <FileMusic size={24} /> : <AudioLines size={24} />}
        <span>
          <b>{kind === 'tja' ? t('messages.tjaChart') : t('messages.oggMp3Audio')}</b>
          <small>
            {file
              ? file.name
              : kind === 'tja'
                ? t('messages.tjaUpTo2Mib')
                : retainedAudioName
                  ? t('messages.keepAudio', { filename: retainedAudioName })
                  : t('messages.oggMp3UpTo100Mib')}
          </small>
        </span>
        {file ? (
          <Check size={18} />
        ) : (
          <span className="text-xs text-muted-foreground">{t('messages.choose')}</span>
        )}
        <Input
          type="file"
          aria-label={
            kind === 'tja' ? t('messages.chooseTjaChart') : t('messages.chooseOggOrMp3Audio')
          }
          accept={kind === 'tja' ? '.tja' : '.ogg,.mp3'}
          disabled={busy}
          onChange={(e) => choose(kind, e.target.files?.[0] ?? null)}
        />
      </Label>
    )
  }
  const existingTitle = existing ? chartText(existing, i18n.resolvedLanguage).title : ''
  return (
    <>
      <div className="[&_p]:mt-2 [&_p]:text-sm [&_p]:text-muted-foreground">
        {!embedded && (
          <h1 className="text-2xl font-extrabold sm:text-3xl">
            {existing
              ? replacing
                ? t('messages.updateSongAndCharts')
                : t('messages.uploadChartFiles')
              : t('db.uploadTitle')}
          </h1>
        )}
        {existing ? (
          <p>
            {replacing
              ? t('messages.replaceSongHint', { title: existingTitle })
              : t('db.uploadChartFilesText')}
          </p>
        ) : (
          mode === 'song' && <p>{t('db.songOnlyText')}</p>
        )}
      </div>
      {!existing && (
        <Tabs
          value={mode}
          onValueChange={(value) => {
            if (busy) return
            requestKey.current = createRequestKey()
            setMode(value as UploadMode)
          }}
        >
          <TabsList aria-label={t('db.uploadMode')}>
            <TabsTrigger value="files">{t('db.uploadModeFiles')}</TabsTrigger>
            <TabsTrigger value="song">{t('db.uploadModeSong')}</TabsTrigger>
          </TabsList>
        </Tabs>
      )}
      {replacing && (
        <Card
          className="min-w-0 gap-3 border border-destructive/30 p-5 shadow-none sm:p-6 [&>h2]:text-destructive [&>p]:text-sm [&>p]:leading-relaxed"
          aria-label={t('messages.beforeUpdating')}
        >
          <h2 className="text-base font-extrabold">
            {t('messages.updatingDeletesAllPreviousScores')}
          </h2>
          <p>{t('messages.theNewTjaReplacesEveryChartInThisSongAllPlayersScores')}</p>
          <p>{t('messages.theSongUrlStaysTheSameTitlesComeFromTheNewTja')}</p>
        </Card>
      )}
      <form className="space-y-6" onSubmit={submit}>
        {mode === 'song' ? (
          <Card className="min-w-0 gap-4 border-[1.5px] border-border p-5 shadow-none sm:p-6">
            <h2 className="text-base font-extrabold">{t('db.songInformation')}</h2>
            <div className="grid gap-4 sm:grid-cols-[1fr_1fr_8rem]">
              <div className="space-y-2">
                <Label htmlFor="upload-title">{t('db.songTitle')}</Label>
                <Input
                  id="upload-title"
                  value={title}
                  maxLength={500}
                  required
                  disabled={busy}
                  onChange={(e) => setTitle(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="upload-subtitle">{t('db.songSubtitle')}</Label>
                <Input
                  id="upload-subtitle"
                  value={subtitle}
                  maxLength={500}
                  disabled={busy}
                  onChange={(e) => setSubtitle(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="upload-bpm">BPM</Label>
                <Input
                  id="upload-bpm"
                  inputMode="decimal"
                  value={bpm}
                  disabled={busy}
                  onChange={(e) => setBpm(e.target.value)}
                />
              </div>
            </div>
            <p className="text-sm text-muted-foreground">{t('db.songFieldsHint')}</p>
            <CoverPicker file={cover} onChange={setCover} disabled={busy} />
          </Card>
        ) : (
          <Card
            className="min-w-0 gap-4 border-[1.5px] border-border p-5 shadow-none sm:p-6"
            onDragOver={(e) => e.preventDefault()}
            onDrop={drop}
          >
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h2 className="text-base font-extrabold">{t('db.uploadFiles')}</h2>
              <span className="text-sm text-muted-foreground">
                {retainedAudioName
                  ? t('messages.newTjaRequiredCurrentAudioCanBeKept')
                  : t('messages.bothFilesAreRequired')}
              </span>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              {fileTile('tja')}
              {fileTile('audio')}
            </div>
            {retainedAudioName && audio && (
              <Button
                type="button"
                variant="ghost"
                size="default"
                className="self-start"
                disabled={busy}
                onClick={() => choose('audio', null)}
              >
                {t('messages.currentAudio', { filename: retainedAudioName })}
              </Button>
            )}
            <p className="text-sm text-muted-foreground">
              {retainedAudioName && !audio ? t('db.replaceWaveHint') : t('db.uploadWaveHint')}
            </p>
            {fileError && (
              <p role="alert" className="text-sm text-destructive">
                {fileError}
              </p>
            )}
            {!fileError && metadata && metadata.sourceEncoding !== 'UTF-8' && (
              <p className="text-sm text-muted-foreground">
                {t('db.tjaConverted', { encoding: metadata.sourceEncoding })}
              </p>
            )}
            <CoverPicker file={cover} onChange={setCover} disabled={busy} />
            <div className="flex min-w-0 flex-col gap-3">
              <h3 className="text-sm font-extrabold">{t('messages.backgroundMovie')}</h3>
              <MoviePicker
                file={movie}
                disabled={busy}
                hint={hasStoredMovie ? t('messages.keepMovie') : t('messages.movieUpTo64Mib')}
                onChange={(file) => {
                  requestKey.current = createRequestKey()
                  setMovie(file)
                }}
              />
            </div>
          </Card>
        )}
        {mode === 'files' && tja && (
          <Card className="min-w-0 gap-4 border-[1.5px] border-border p-5 shadow-none sm:p-6">
            <h2 className="text-base font-extrabold">{t('messages.previewRange')}</h2>
            <div className="grid gap-4 sm:grid-cols-2">
              <Label htmlFor="upload-demo-start" className="flex flex-col items-start gap-2">
                {t('messages.previewStartSeconds')}
                <Input
                  id="upload-demo-start"
                  type="number"
                  min="0"
                  step="any"
                  value={demoStart}
                  disabled={busy}
                  onChange={(e) => setDemoStart(e.target.value)}
                />
              </Label>
              <Label htmlFor="upload-demo-end" className="flex flex-col items-start gap-2">
                {t('messages.previewEndSeconds')}
                <Input
                  id="upload-demo-end"
                  type="number"
                  min="0"
                  max="1215"
                  step="any"
                  value={demoEnd}
                  disabled={busy}
                  onChange={(e) => setDemoEnd(e.target.value)}
                />
              </Label>
            </div>
            {(audioUrl || retainedAudioName) && (
              <AudioPlayer
                label={t('messages.audioPreview')}
                src={audioUrl || endpoints.resource(existing!, 'audio')}
                startAt={demoStart.trim() ? Number(demoStart) : NaN}
                endAt={demoEnd.trim() ? Number(demoEnd) : NaN}
                knownDuration={audio ? (audioDuration ?? undefined) : existing?.duration}
                disabled={busy || Number(demoEnd) > 1215}
              />
            )}
          </Card>
        )}
        {mode === 'files' && metadata && (
          <Card className="min-w-0 gap-4 border-[1.5px] border-border p-5 shadow-none sm:p-6">
            <h2 className="text-base font-extrabold">{t('messages.difficultiesAndCreators')}</h2>
            <p className="text-sm text-muted-foreground">
              {metadata.maker
                ? t('messages.creatorNamesAreFilledFromMakerYouCanEditEachOne')
                : t('messages.noMakerWasSpecifiedEnterACreatorForEachDifficulty')}
            </p>
            <Table className="w-full">
              <TableHeader>
                <TableRow>
                  <TableHead scope="col">{t('messages.difficulty')}</TableHead>
                  <TableHead scope="col">{t('messages.creator')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {metadata.difficulties.map((d) => (
                  <TableRow key={slotKey(d)}>
                    <TableHead scope="row">
                      {blockName(d)} ★{d.level}
                    </TableHead>
                    <TableCell>
                      <Input
                        aria-label={t('messages.courseCreator', { difficulty: blockName(d) })}
                        value={d.maker}
                        maxLength={500}
                        placeholder={t('messages.notSpecified')}
                        disabled={busy}
                        onChange={(e) => {
                          const maker = e.target.value
                          setTypedMakers((typed) => ({ ...typed, [slotKey(d)]: maker }))
                          setMetadata(
                            (current) =>
                              current && {
                                ...current,
                                difficulties: current.difficulties.map((block) =>
                                  slotKey(block) === slotKey(d) ? { ...block, maker } : block,
                                ),
                              },
                          )
                          requestKey.current = createRequestKey()
                        }}
                      />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>
        )}
        <Card className="min-w-0 gap-5 border-[1.5px] border-border p-5 shadow-none sm:p-6">
          <h2 className="text-base font-extrabold">{t('db.record')}</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="upload-nijiiro-id">{t('messages.nijiiroId')}</Label>
              <Input
                id="upload-nijiiro-id"
                inputMode="numeric"
                value={nijiiroId}
                disabled={busy}
                onChange={(e) => setNijiiroId(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="upload-official-id">{t('messages.officialId')}</Label>
              <Input
                id="upload-official-id"
                value={officialId}
                maxLength={64}
                disabled={busy}
                onChange={(e) => setOfficialId(e.target.value)}
              />
            </div>
          </div>
          <p className="text-sm text-muted-foreground">{t('db.identifierHint')}</p>
          <CategoryPicker value={categoryIds} onChange={setCategoryIds} disabled={busy} />
          <div className="space-y-2">
            <Label htmlFor="upload-description">{t('messages.notes')}</Label>
            <Textarea
              id="upload-description"
              rows={4}
              maxLength={1000}
              value={description}
              disabled={busy}
              onChange={(e) => setDescription(e.target.value)}
            />
            <p className="text-right text-xs text-muted-foreground">{description.length} / 1000</p>
          </div>
        </Card>
        {busy && (
          <div className="space-y-2" role="status">
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
              <LoaderCircle className="size-4 animate-spin" aria-hidden="true" />
              {existing && replacing ? t('messages.updatingSong') : t('messages.publishingChart')}
            </p>
            <Progress value={progress} />
          </div>
        )}
        <div className="flex flex-wrap gap-3">
          <Button type="submit" size="lg" disabled={!ready || busy || !!fileError}>
            <Upload size={17} aria-hidden="true" />
            {existing
              ? replacing
                ? t('messages.updateSongAndCharts')
                : t('db.upload')
              : mode === 'song'
                ? t('db.registerSong')
                : t('db.upload')}
          </Button>
          {busy ? (
            <Button
              type="button"
              variant="outline"
              size="lg"
              onClick={() => controller.current?.abort()}
            >
              {t('messages.cancel')}
            </Button>
          ) : (
            existing && (
              <Link
                className={buttonVariants({ variant: 'outline', size: 'lg' })}
                to={`/charts/${existing.id}`}
              >
                {t('messages.cancel')}
              </Link>
            )
          )}
        </div>
      </form>
      {reviewing && existing && (
        <ReplacementReview
          review={reviewing.review}
          files={{
            tja: tja?.name ?? '',
            audio: audio && { name: audio.name, changed: reviewing.audioChanged },
            keptAudio: existing.audioName,
            cover: cover?.name ?? null,
            movie: movie
              ? { name: movie.name }
              : !metadata?.movie && hasStoredMovie
                ? 'remove'
                : null,
          }}
          onCancel={() => {
            controller.current?.abort()
            setReviewing(null)
          }}
          onConfirm={(kept) => void save(kept)}
        />
      )}
    </>
  )
}
