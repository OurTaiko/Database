import { useTranslation } from 'react-i18next'
import { chartText } from '@/chart-language'
import { i18n } from '@/i18n'
import { endpoints } from '@/api/endpoints'
import { Card } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Button, buttonVariants } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Progress } from '@/components/ui/progress'
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
import { CoverPicker } from '@/components/cover-picker'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { CategoryPicker } from './categories'
import { api, previewReplacement, uploadChart } from './api/client'
import type { Chart } from './api/types'
import { EmptyState } from '@/components/empty-state'
import { Notice } from './notifications'
import { useNotification } from './notification-context'
import { useSession } from './session-context'
import { isAudioFilename, maxAudio, maxTja } from './tja'
import { officialIdPattern, parseBpm, parseNijiiroId } from './edit-metadata'
import { audioDiffers, replacementFields, reviewReplacement } from './replacement-diff'
import type { Proposal, ReplacementPreview, Review } from './replacement-diff'
import { ReplacementReview } from './replacement-review'

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
    api<Chart>(endpoints.chart(id), { signal: controller.signal })
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
    (session.user.isAdmin || (session.user.canUpload && session.user.id === chart.uploaderId))
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
  useEffect(() => () => controller.current?.abort(), [])
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
      tracks = files.filter((f) => isAudioFilename(f.name))
    if (charts.length > 1 || tracks.length > 1 || charts.length + tracks.length !== files.length) {
      notify(t('messages.dropOneTjaAndOneOggOrMp3NoAdditionalFiles'), 'error')
      return
    }
    if (charts[0]) choose('tja', charts[0])
    if (tracks[0]) choose('audio', tracks[0])
  }
  const fileError =
    mode === 'song'
      ? ''
      : tja && (!tja.size || tja.size > maxTja)
        ? t('db.tjaTooLarge')
        : audio && (!audio.size || audio.size > maxAudio)
          ? t('db.audioTooLarge')
          : ''
  const ready = mode === 'song' ? !!title.trim() : !!tja && (!!audio || !!retainedAudioName)
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
    if (replacing) await startReview()
    else await save()
  }
  // The preview range and makers follow the file here; the record is what the form says.
  const proposal = (): Proposal => ({
    description,
    categoryIds,
    nijiiroId: nijiiroId.trim(),
    officialId: officialId.trim(),
    demoStart: 0,
    demoEnd: 0,
    typedMakers: {},
    typedDemo: false,
  })
  const startReview = async () => {
    const abort = new AbortController()
    controller.current = abort
    setReviewing({ preview: null, review: null, audioChanged: false })
    try {
      const [preview, audioChanged] = await Promise.all([
        previewReplacement(existing!.id, tja!, audio?.name, session.csrfToken, abort.signal),
        audioDiffers(existing!, audio),
      ])
      if (abort.signal.aborted) return
      setReviewing({
        preview,
        audioChanged,
        review: reviewReplacement(existing!, preview, proposal(), audioChanged, {
          demo: false,
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
        form.append('tja', tja!)
        if (audio) form.append('audio', audio)
      }
      if (cover) form.append('cover', cover)
      if (replacing) form.append('confirmReset', 'true')
      if (fields.keepTitles) form.append('keepTitles', 'true')
      form.append('description', fields.description)
      form.append('categoryIds', JSON.stringify(fields.categoryIds))
      form.append('nijiiroId', fields.nijiiroId)
      form.append('officialId', fields.officialId)
      const chart = await uploadChart(
        form,
        session.csrfToken,
        requestKey.current,
        abort.signal,
        setProgress,
        existing?.id,
      )
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
            <CoverPicker file={cover} onChange={setCover} disabled={busy} />
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
            movie: null,
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
