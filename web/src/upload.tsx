import { useTranslation } from 'react-i18next'
import { Card } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Button, buttonVariants } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Progress } from '@/components/ui/progress'
import { useEffect, useRef, useState } from 'react'
import type { DragEvent, FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
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
import { uploadChart } from './api/client'
import { useNotification } from './notification-context'
import { useSession } from './session-context'
import { isAudioFilename, maxAudio, maxTja } from './tja'
import { officialIdPattern, parseBpm, parseNijiiroId } from './edit-metadata'

function createRequestKey() {
  // getRandomValues remains available over LAN HTTP, unlike randomUUID.
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}
const emptyStateClassName =
  'flex flex-col items-center justify-center gap-4 rounded-[14px] border border-dashed bg-card/60 px-6 py-16 text-center [&>p]:max-w-lg [&>p]:text-muted-foreground'

type UploadMode = 'files' | 'song'

// Adds an official chart: a TJA, its audio and what identifies the song. The
// API parses and validates the files; this page only checks what it can see.
// In song mode no files are sent: a song that is known but whose files are
// not at hand is registered by its title, and its files are uploaded later
// from its page.
export function UploadPage() {
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
  const [nijiiroId, setNijiiroId] = useState(''),
    [officialId, setOfficialId] = useState('')
  const [categoryIds, setCategoryIds] = useState<string[]>([])
  const [description, setDescription] = useState(''),
    [busy, setBusy] = useState(false),
    [progress, setProgress] = useState(0)
  const requestKey = useRef(createRequestKey()),
    controller = useRef<AbortController | null>(null)
  useEffect(() => () => controller.current?.abort(), [])
  if (session.loading) return <p role="status">{t('messages.loadingAccount')}</p>
  // Only users holding the upload permission get the upload interface.
  if (!session.user?.canUpload)
    return (
      <div className={emptyStateClassName}>
        <h1 className="text-2xl font-extrabold sm:text-3xl">
          {t('db.uploadUnavailable')}
        </h1>
        <p>{session.user ? t('db.uploadNeedsPermission') : t('db.uploadNeedsSignIn')}</p>
        {!session.user && (
          <Link
            className={buttonVariants({ variant: 'default', size: 'default' })}
            to="/login?returnTo=/upload"
          >
            {t('messages.signIn')}
          </Link>
        )}
      </div>
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
  const ready = mode === 'song' ? !!title.trim() : !!tja && !!audio
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
        form.append('audio', audio!)
      }
      if (cover) form.append('cover', cover)
      form.append('description', description)
      form.append('categoryIds', JSON.stringify(categoryIds))
      form.append('nijiiroId', nijiiroId.trim())
      form.append('officialId', official)
      const chart = await uploadChart(
        form,
        session.csrfToken,
        requestKey.current,
        abort.signal,
        setProgress,
      )
      navigate(`/charts/${chart.id}`)
      notify(
        mode === 'song' ? t('messages.songRegistered') : t('messages.chartPublished'),
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
  return (
    <>
      <div className="[&_p]:mt-2 [&_p]:text-sm [&_p]:text-muted-foreground">
        <h1 className="text-2xl font-extrabold sm:text-3xl">{t('db.uploadTitle')}</h1>
        {mode === 'song' && <p>{t('db.songOnlyText')}</p>}
      </div>
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
            <h2 className="text-base font-extrabold">{t('db.uploadFiles')}</h2>
            <div className="grid gap-4 sm:grid-cols-2">
              {fileTile('tja')}
              {fileTile('audio')}
            </div>
            <p className="text-sm text-muted-foreground">{t('db.uploadWaveHint')}</p>
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
              {t('messages.publishingChart')}
            </p>
            <Progress value={progress} />
          </div>
        )}
        <div className="flex flex-wrap gap-3">
          <Button type="submit" size="lg" disabled={!ready || busy || !!fileError}>
            <Upload size={17} aria-hidden="true" />
            {mode === 'song' ? t('db.registerSong') : t('db.upload')}
          </Button>
          {busy && (
            <Button
              type="button"
              variant="outline"
              size="lg"
              onClick={() => controller.current?.abort()}
            >
              {t('messages.cancel')}
            </Button>
          )}
        </div>
      </form>
    </>
  )
}
