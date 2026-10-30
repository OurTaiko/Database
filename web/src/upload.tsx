import { isChartOwner } from '@sitekit/chart-owners'
import { useTranslation } from 'react-i18next'
import { chartText } from '@sitekit/chart-language'
import { i18n } from '@sitekit/i18n'
import { endpoints } from '@/api/endpoints'
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
import { CategoryPicker } from './categories'
import { api, previewReplacement, uploadChart } from '@sitekit/api/client'
import type { Chart } from './api/types'
import { EmptyState } from '@sitekit/components/empty-state'
import { Notice } from '@sitekit/notifications'
import { useNotification } from '@sitekit/notification-context'
import { useSession } from './session-context'
import { isAudioFilename, isMovieFilename, maxAudio, maxTja } from './tja'
import { normalizeTja } from '@sitekit/tja-encoding'
import { officialIdPattern, parseBpm, parseNijiiroId } from './edit-metadata'
import { audioDiffers, replacementFields, reviewReplacement } from '@sitekit/replacement-diff'
import type { Proposal, ReplacementPreview, Review } from '@sitekit/replacement-diff'
import { ReplacementReview } from './replacement-review'

function createRequestKey() {
  // getRandomValues remains available over LAN HTTP, unlike randomUUID.
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}
type UploadMode = 'files' | 'song'

/**
 * What the server reads from the TJA's headers before the first #START: DEMOSTART (0 when
 * absent) and whether BGMOVIE names a movie. The server checks the file; this only fills the form.
 */
async function fileHeaders(tja: File) {
  // The headers are ASCII in every encoding the site accepts, so Latin-1 reads them.
  const text = new TextDecoder('latin1').decode(await tja.slice(0, 256 * 1024).arrayBuffer())
  const header = text.split(/#START/i)[0]
  const demo = /^\s*DEMOSTART\s*:\s*([+-]?(?:\d+(?:\.\d*)?|\.\d+))\s*$/im.exec(header)
  return {
    demoStart: demo ? Number(demo[1]) : 0,
    movie: /^\s*BGMOVIE\s*:\s*\S/m.test(header),
  }
}

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
  const canManage =
    !!session.user &&
    (session.user.isAdmin || (session.user.canUpload && isChartOwner(chart, session.user.id)))
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
  // The preview range starts as the new TJA's DEMOSTART for 15 seconds, as on Fanmade.
  const [fileDemo, setFileDemo] = useState<number | null>(null)
  // The new TJA declares a background movie.
  const [declaresMovie, setDeclaresMovie] = useState(false)
  const [movie, setMovie] = useState<File | null>(null)
  const hasStoredMovie = replacing && !!existing?.movieHash
  const [demoStart, setDemoStart] = useState('0'),
    [demoEnd, setDemoEnd] = useState('15')
  // The TJA as uploaded: the site takes UTF-8 only, so the page converts other encodings first.
  const [utf8Tja, setUtf8Tja] = useState<{ file: File; sourceEncoding: string } | null>(null),
    [encodingError, setEncodingError] = useState('')
  const [audioUrl, setAudioUrl] = useState('')
  const [audioDuration, setAudioDuration] = useState<number | null>(null)
  useEffect(() => () => controller.current?.abort(), [])
  useEffect(() => {
    let active = true
    setFileDemo(null)
    setDeclaresMovie(false)
    if (!tja) return
    void fileHeaders(tja).then(({ demoStart: start, movie: declared }) => {
      if (!active) return
      setFileDemo(start)
      setDeclaresMovie(declared)
      setDemoStart(String(start))
      setDemoEnd(String(start + 15))
    })
    return () => {
      active = false
    }
  }, [tja])
  // WAVE names the audio, which helps tell legacy encodings apart.
  const waveName = audio?.name ?? retainedAudioName ?? ''
  useEffect(() => {
    let active = true
    setUtf8Tja(null)
    setEncodingError('')
    if (!tja || !tja.size || tja.size > maxTja) return
    void tja
      .arrayBuffer()
      .then((bytes) => normalizeTja(new Uint8Array(bytes), waveName))
      .then(
        ({ data, sourceEncoding }) => {
          if (!active) return
          const file = new File([data], tja.name, { type: 'text/plain;charset=utf-8' })
          setUtf8Tja({ file, sourceEncoding })
        },
        (e: Error) => {
          if (active) setEncodingError(e.message)
        },
      )
    return () => {
      active = false
    }
  }, [tja, waveName])
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
  // Only users holding the upload permission get the upload interface.
  if (!session.user?.canUpload)
    return (
      <EmptyState title={t('db.uploadUnavailable')}>
        <p className="text-muted-foreground text-sm">
          {session.user ? t('db.uploadNeedsPermission') : t('db.uploadNeedsSignIn')}
        </p>
        {!session.user && (
          <Link
            className={buttonVariants({ variant: 'default', size: 'default' })}
            to="/login?returnTo=/me/charts/publish"
          >
            {t('messages.signIn')}
          </Link>
        )}
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
  const fileError =
    mode === 'song'
      ? ''
      : tja && (!tja.size || tja.size > maxTja || (utf8Tja && utf8Tja.file.size > maxTja))
        ? t('db.tjaTooLarge')
        : encodingError
          ? encodingError
          : audio && (!audio.size || audio.size > maxAudio)
            ? t('db.audioTooLarge')
            : ''
  const ready = mode === 'song' ? !!title.trim() : !!utf8Tja && (!!audio || !!retainedAudioName)
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
    fileDemo === null || demoStart !== String(fileDemo) || demoEnd !== String(fileDemo + 15)
  // Makers always follow the file here; the record is what the form says.
  const proposal = (): Proposal => ({
    description,
    categoryIds,
    nijiiroId: nijiiroId.trim(),
    officialId: officialId.trim(),
    demoStart: Number(demoStart),
    demoEnd: Number(demoEnd),
    typedMakers: {},
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
          utf8Tja!.file,
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
          makers: false,
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
        form.append('tja', utf8Tja!.file)
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
          ? t(replacing ? 'db.chartFilesReplaced' : 'messages.chartFilesUploaded')
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
                : t('db.uploadChartFiles')
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
            {!fileError && utf8Tja && utf8Tja.sourceEncoding !== 'UTF-8' && (
              <p className="text-sm text-muted-foreground">
                {t('db.tjaConverted', { encoding: utf8Tja.sourceEncoding })}
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
        <Card className="min-w-0 gap-5 border-[1.5px] border-border p-5 shadow-none sm:p-6">
          <h2 className="text-base font-extrabold">{t('db.record')}</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="upload-nijiiro-id">{t('db.nijiiroId')}</Label>
              <Input
                id="upload-nijiiro-id"
                inputMode="numeric"
                value={nijiiroId}
                disabled={busy}
                onChange={(e) => setNijiiroId(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="upload-official-id">{t('db.officialId')}</Label>
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
            <Label htmlFor="upload-description">{t('db.notes')}</Label>
            <Textarea
              id="upload-description"
              rows={4}
              value={description}
              disabled={busy}
              onChange={(e) => setDescription(e.target.value)}
            />
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
              : !declaresMovie && hasStoredMovie
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
