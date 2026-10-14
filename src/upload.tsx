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
import { CategoryPicker } from './categories'
import { uploadChart } from './api/client'
import { useNotification } from './notification-context'
import { useSession } from './session-context'
import { isAudioFilename, maxAudio, maxTja } from './tja'
import { officialIdPattern, parseNijiiroId } from './edit-metadata'

function createRequestKey() {
  // getRandomValues remains available over LAN HTTP, unlike randomUUID.
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}
const emptyStateClassName =
  'flex flex-col items-center justify-center gap-4 rounded-3xl border border-dashed bg-card/60 px-6 py-16 text-center [&>p]:max-w-lg [&>p]:text-muted-foreground'

// Adds an official chart: a TJA, its audio and what identifies the song. The
// API parses and validates the files; this page only checks what it can see.
export function UploadPage() {
  const { t } = useTranslation()
  const { notify } = useNotification()
  const session = useSession(),
    navigate = useNavigate()
  const [tja, setTja] = useState<File | null>(null),
    [audio, setAudio] = useState<File | null>(null),
    [cover, setCover] = useState<File | null>(null)
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
        <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">
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
    tja && (!tja.size || tja.size > maxTja)
      ? t('db.tjaTooLarge')
      : audio && (!audio.size || audio.size > maxAudio)
        ? t('db.audioTooLarge')
        : ''
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!tja || !audio || busy || fileError) return
    const official = officialId.trim()
    if (
      parseNijiiroId(nijiiroId) === undefined ||
      (official && !officialIdPattern.test(official))
    ) {
      notify(t('db.identifierInvalid'), 'error')
      return
    }
    setBusy(true)
    setProgress(0)
    const abort = new AbortController()
    controller.current = abort
    try {
      const form = new FormData()
      form.append('tja', tja)
      form.append('audio', audio)
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
      notify(t('messages.chartPublished'), 'success')
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
      <Label className="flex min-w-0 flex-col items-start gap-3 rounded-2xl border p-4 [&>span]:w-full [&_small]:mt-1 [&_small]:block [&_small]:truncate [&_small]:text-muted-foreground">
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
        <h1 className="text-2xl font-semibold tracking-tight sm:text-3xl">{t('db.uploadTitle')}</h1>
        <p>{t('db.uploadText')}</p>
      </div>
      <form className="space-y-6" onSubmit={submit}>
        <Card
          className="min-w-0 gap-4 border p-5 shadow-none ring-0 sm:p-6"
          onDragOver={(e) => e.preventDefault()}
          onDrop={drop}
        >
          <h2 className="text-base font-semibold">{t('db.uploadFiles')}</h2>
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
        <Card className="min-w-0 gap-5 border p-5 shadow-none ring-0 sm:p-6">
          <h2 className="text-base font-semibold">{t('db.record')}</h2>
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
          <Button type="submit" size="lg" disabled={!tja || !audio || busy || !!fileError}>
            <Upload size={17} aria-hidden="true" />
            {t('db.upload')}
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
