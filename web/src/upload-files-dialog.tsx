import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  WaveformIcon as AudioLines,
  FileAudioIcon as FileMusic,
  CheckIcon as Check,
} from '@phosphor-icons/react'
import { DialogClose } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import { uploadChart } from './api/client'
import type { Chart } from './api/types'
import { Modal } from './notifications'
import { useSession } from './session-context'
import { maxAudio, maxTja } from './tja'

function createRequestKey() {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

/**
 * Gives a song registered without files its TJA and audio, through the same
 * replacement as editing a chart's files. Both files are required here; the
 * title, subtitle, BPM and blocks then come from the TJA.
 */
export function UploadFilesDialog({
  chart,
  onSaved,
  onDismiss,
}: {
  chart: Chart
  onSaved: (chart: Chart) => void
  onDismiss: () => void
}) {
  const { t } = useTranslation()
  const session = useSession()
  const [tja, setTja] = useState<File | null>(null)
  const [audio, setAudio] = useState<File | null>(null)
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState(0)
  const [error, setError] = useState('')
  const requestKey = useRef(createRequestKey())
  const controller = useRef<AbortController | null>(null)
  useEffect(() => () => controller.current?.abort(), [])
  const fileError =
    tja && (!tja.size || tja.size > maxTja)
      ? t('db.tjaTooLarge')
      : audio && (!audio.size || audio.size > maxAudio)
        ? t('db.audioTooLarge')
        : ''
  const choose = (kind: 'tja' | 'audio', file: File | null) => {
    requestKey.current = createRequestKey()
    if (kind === 'tja') setTja(file)
    else setAudio(file)
  }
  const save = async () => {
    if (!tja || !audio || busy || fileError) return
    setBusy(true)
    setError('')
    setProgress(0)
    const abort = new AbortController()
    controller.current = abort
    try {
      const form = new FormData()
      form.append('tja', tja)
      form.append('audio', audio)
      if (chart.hasFiles) form.append('confirmReset', 'true')
      const updated = await uploadChart(
        form,
        session.csrfToken,
        requestKey.current,
        abort.signal,
        setProgress,
        chart.id,
      )
      onSaved(updated)
      onDismiss()
    } catch (e) {
      if (!abort.signal.aborted) setError((e as Error).message)
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
    <Modal
      title={t('db.uploadChartFiles')}
      busy={busy}
      onDismiss={onDismiss}
      className="sm:max-w-2xl"
      actions={
        <>
          <DialogClose render={<Button type="button" variant="outline" />} disabled={busy}>
            {t('messages.cancel')}
          </DialogClose>
          <Button type="button" disabled={!tja || !audio || busy || !!fileError} onClick={save}>
            {busy ? t('messages.publishingChart') : t('db.upload')}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-muted-foreground">{t('db.uploadChartFilesText')}</p>
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
        {busy && <Progress value={progress} aria-label={t('messages.publishingChart')} />}
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
      </div>
    </Modal>
  )
}
