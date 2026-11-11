import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import type { FormEvent } from 'react'
import { DialogClose } from '@sitekit/components/ui/dialog'
import { Button } from '@sitekit/components/ui/button'
import { Input } from '@sitekit/components/ui/input'
import { Label } from '@sitekit/components/ui/label'
import { Textarea } from '@sitekit/components/ui/textarea'
import { endpoints } from '@sitekit/api/endpoints'
import { AudioPlayer } from '@sitekit/components/audio-player'
import { api, jsonRequest, replaceMovie } from '@sitekit/api/client'
import { MovieSettings, movieChanges, movieDraft } from '@sitekit/components/movie-settings'
import { useNotification } from '@sitekit/notification-context'
import type { Chart, Locale } from './api/types'
import { CategoryPicker } from '@sitekit/categories'
import { cn } from '@sitekit/lib/utils'
import {
  NijiiroIdField,
  nijiiroIdBlocks,
  parseNijiiroId,
  useNijiiroIdCheck,
} from '@sitekit/nijiiro-id'
import { Modal } from '@sitekit/notifications'

const locales: Locale[] = ['ja', 'en', 'zh', 'ko']
const localeNames: Record<Locale, string> = {
  ja: '日本語',
  en: 'English',
  zh: '中文',
  ko: '한국어',
}

export const officialIdPattern = /^[A-Za-z0-9_.-]{1,64}$/
/** Reads an optional BPM; `undefined` means invalid, `null` unknown. */
export function parseBpm(value: string): number | null | undefined {
  const text = value.trim()
  if (!text) return null
  const n = Number(text)
  return Number.isFinite(n) && n > 0 && n <= 10000 ? n : undefined
}

// Edits what the database says about a chart. The stored TJA and audio are not
// changed here, but for the background movie and MOVIEOFFSET (R057), which are
// rewritten in it. The title, subtitle and preview range are the database's at
// any time (R042, R045); the BPM and difficulties are the TJA's, so the BPM is
// editable only on a song registered without files.
export function EditMetadata({
  chart,
  csrf,
  onSaved,
  onDismiss,
}: {
  chart: Chart
  csrf: string
  onSaved: (chart: Chart) => void
  onDismiss: () => void
}) {
  const { t } = useTranslation()
  const [title, setTitle] = useState(chart.title)
  const [subtitle, setSubtitle] = useState(chart.subtitle)
  const [bpm, setBpm] = useState(chart.bpm > 0 ? String(chart.bpm) : '')
  const [nijiiroId, setNijiiroId] = useState(chart.nijiiroId?.toString() ?? '')
  const nijiiroCheck = useNijiiroIdCheck(nijiiroId, chart.id)
  const [officialId, setOfficialId] = useState(chart.officialId ?? '')
  const [categoryIds, setCategoryIds] = useState(chart.categoryIds)
  const [description, setDescription] = useState(chart.description)
  const [titles, setTitles] = useState(chart.titleTranslations)
  const [subtitles, setSubtitles] = useState(chart.subtitleTranslations)
  const [demoStart, setDemoStart] = useState(String(chart.demoStart))
  const [demoEnd, setDemoEnd] = useState(String(chart.demoEnd))
  const [movie, setMovie] = useState(() => movieDraft(chart))
  const movieEdit = movieChanges(chart, movie)
  const { notify } = useNotification()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const nijiiro = parseNijiiroId(nijiiroId)
    const official = officialId.trim()
    if (nijiiro === undefined || (official && !officialIdPattern.test(official))) {
      setError(t('db.identifierInvalid'))
      return
    }
    if (nijiiroIdBlocks(nijiiroCheck)) {
      setError(t('errors.NIJIIRO_ID_TAKEN'))
      return
    }
    const parsedBpm = parseBpm(bpm)
    if (!title.trim() || (!chart.hasFiles && parsedBpm === undefined)) {
      setError(t('db.songFieldsInvalid'))
      return
    }
    const start = Number(demoStart),
      end = Number(demoEnd)
    const demoChanged = start !== chart.demoStart || end !== chart.demoEnd
    if (
      chart.hasFiles &&
      demoChanged &&
      (!demoStart.trim() ||
        !demoEnd.trim() ||
        !Number.isFinite(start) ||
        !Number.isFinite(end) ||
        start < 0 ||
        start >= chart.duration ||
        end <= start ||
        end > 1215)
    ) {
      setError(t('messages.previewRangeInvalid'))
      return
    }
    if (chart.hasFiles && movieEdit.invalid) {
      setError(t('messages.movieOffsetInvalid'))
      return
    }
    setBusy(true)
    setError('')
    try {
      // The movie first: a rejected file leaves the other edits unsaved.
      if (chart.hasFiles && movieEdit.file)
        await replaceMovie(chart.id, movieEdit.file, movieEdit.offset, csrf)
      const translations = (values: Partial<Record<Locale, string>>) =>
        Object.fromEntries(locales.map((locale) => [locale, values[locale]?.trim() || null]))
      onSaved(
        await api<Chart>(
          endpoints.chart(chart.id),
          jsonRequest(
            'PATCH',
            {
              title: title.trim(),
              subtitle: subtitle.trim(),
              // The BPM is the TJA's once a chart has files.
              ...(chart.hasFiles ? {} : { bpm: parsedBpm }),
              // The preview range needs the audio; it is sent only when changed.
              ...(chart.hasFiles && demoChanged ? { demoStart: start, demoEnd: end } : {}),
              ...(chart.hasFiles && !movieEdit.file && movieEdit.offset !== undefined
                ? { movieOffset: movieEdit.offset }
                : {}),
              nijiiroId: nijiiro,
              officialId: official || null,
              categoryIds,
              description,
              titleTranslations: translations(titles),
              subtitleTranslations: translations(subtitles),
            },
            csrf,
          ),
        ),
      )
      if (chart.hasFiles && movieEdit.file) notify(t('messages.movieSubmitted'), 'info')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal
      title={t('messages.editInformation')}
      busy={busy}
      onDismiss={onDismiss}
      actions={null}
      className="sm:max-w-2xl"
    >
      <form className="space-y-5" onSubmit={submit}>
        <div
          className={cn(
            'grid gap-4',
            chart.hasFiles ? 'sm:grid-cols-2' : 'sm:grid-cols-[1fr_1fr_8rem]',
          )}
        >
          <div className="space-y-2">
            <Label htmlFor="edit-title">{t('db.songTitle')}</Label>
            <Input
              id="edit-title"
              value={title}
              maxLength={500}
              required
              disabled={busy}
              onChange={(e) => setTitle(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="edit-subtitle">{t('db.songSubtitle')}</Label>
            <Input
              id="edit-subtitle"
              value={subtitle}
              maxLength={500}
              disabled={busy}
              onChange={(e) => setSubtitle(e.target.value)}
            />
          </div>
          {!chart.hasFiles && (
            <div className="space-y-2">
              <Label htmlFor="edit-bpm">BPM</Label>
              <Input
                id="edit-bpm"
                inputMode="decimal"
                value={bpm}
                disabled={busy}
                onChange={(e) => setBpm(e.target.value)}
              />
            </div>
          )}
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <NijiiroIdField
            id="edit-nijiiro-id"
            value={nijiiroId}
            check={nijiiroCheck}
            disabled={busy}
            onChange={setNijiiroId}
          />
          <div className="space-y-2">
            <Label htmlFor="edit-official-id">{t('messages.officialId')}</Label>
            <Input
              id="edit-official-id"
              value={officialId}
              maxLength={64}
              disabled={busy}
              onChange={(e) => setOfficialId(e.target.value)}
            />
          </div>
        </div>
        <p className="text-sm text-muted-foreground">{t('db.identifierHint')}</p>
        {chart.hasFiles && (
          <fieldset disabled={busy} className="space-y-3 rounded-[14px] border p-4">
            <legend className="px-2 text-sm font-medium">{t('messages.previewRange')}</legend>
            <div className="grid gap-4 sm:grid-cols-2">
              <Label htmlFor="edit-demo-start" className="flex flex-col items-start gap-2">
                {t('messages.previewStartSeconds')}
                <Input
                  id="edit-demo-start"
                  type="number"
                  min="0"
                  step="any"
                  required
                  value={demoStart}
                  onChange={(e) => setDemoStart(e.target.value)}
                />
              </Label>
              <Label htmlFor="edit-demo-end" className="flex flex-col items-start gap-2">
                {t('messages.previewEndSeconds')}
                <Input
                  id="edit-demo-end"
                  type="number"
                  min="0"
                  max="1215"
                  step="any"
                  required
                  value={demoEnd}
                  onChange={(e) => setDemoEnd(e.target.value)}
                />
              </Label>
            </div>
            <AudioPlayer
              label={t('messages.audioPreview')}
              src={endpoints.resource(chart, 'audio')}
              startAt={demoStart.trim() ? Number(demoStart) : NaN}
              endAt={demoEnd.trim() ? Number(demoEnd) : NaN}
              knownDuration={chart.duration}
              disabled={busy || Number(demoEnd) > 1215}
            />
          </fieldset>
        )}
        {chart.hasFiles && (
          <MovieSettings chart={chart} draft={movie} onChange={setMovie} disabled={busy} />
        )}
        <CategoryPicker value={categoryIds} onChange={setCategoryIds} disabled={busy} />
        <fieldset className="space-y-3 [&>legend]:mb-2 [&>legend]:text-sm [&>legend]:font-medium">
          <legend>{t('db.translations')}</legend>
          {locales.map((locale) => (
            <div key={locale} className="grid gap-2 sm:grid-cols-[5rem_1fr_1fr] sm:items-center">
              <span className="text-sm text-muted-foreground">{localeNames[locale]}</span>
              <Input
                aria-label={`${t('db.translatedTitle')} · ${localeNames[locale]}`}
                placeholder={t('db.translatedTitle')}
                value={titles[locale] ?? ''}
                maxLength={500}
                disabled={busy}
                onChange={(e) => setTitles({ ...titles, [locale]: e.target.value })}
              />
              <Input
                aria-label={`${t('db.translatedSubtitle')} · ${localeNames[locale]}`}
                placeholder={t('db.translatedSubtitle')}
                value={subtitles[locale] ?? ''}
                maxLength={500}
                disabled={busy}
                onChange={(e) => setSubtitles({ ...subtitles, [locale]: e.target.value })}
              />
            </div>
          ))}
        </fieldset>
        <div className="space-y-2">
          <Label htmlFor="edit-description">{t('db.notes')}</Label>
          <Textarea
            id="edit-description"
            rows={4}
            value={description}
            disabled={busy}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <DialogClose render={<Button type="button" variant="outline" />} disabled={busy}>
            {t('messages.cancel')}
          </DialogClose>
          <Button type="submit" disabled={busy}>
            {busy ? t('messages.saving') : t('messages.saveChanges')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
