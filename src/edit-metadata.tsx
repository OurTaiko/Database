import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import type { FormEvent } from 'react'
import { DialogClose } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { endpoints } from '@/api/endpoints'
import { api, jsonRequest } from './api/client'
import type { Chart, Locale } from './api/types'
import { CategoryPicker } from './categories'
import { Modal } from './notifications'

const locales: Locale[] = ['ja', 'en', 'zh', 'ko']
const localeNames: Record<Locale, string> = {
  ja: '日本語',
  en: 'English',
  zh: '中文',
  ko: '한국어',
}

/** Reads the identifier fields; `undefined` means the Nijiiro ID is invalid. */
export function parseNijiiroId(value: string): number | null | undefined {
  const text = value.trim()
  if (!text) return null
  return /^[1-9][0-9]{0,8}$/.test(text) ? Number(text) : undefined
}
export const officialIdPattern = /^[A-Za-z0-9_.-]{1,64}$/

// Edits what the database says about a chart. The stored TJA and audio, and
// with them the title, BPM and difficulties, are not changed here.
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
  const [nijiiroId, setNijiiroId] = useState(chart.nijiiroId?.toString() ?? '')
  const [officialId, setOfficialId] = useState(chart.officialId ?? '')
  const [categoryIds, setCategoryIds] = useState(chart.categoryIds)
  const [description, setDescription] = useState(chart.description)
  const [titles, setTitles] = useState(chart.titleTranslations)
  const [subtitles, setSubtitles] = useState(chart.subtitleTranslations)
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
    setBusy(true)
    setError('')
    try {
      const translations = (values: Partial<Record<Locale, string>>) =>
        Object.fromEntries(locales.map((locale) => [locale, values[locale]?.trim() || null]))
      onSaved(
        await api<Chart>(
          endpoints.chart(chart.id),
          jsonRequest(
            'PATCH',
            {
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
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="edit-nijiiro-id">{t('db.nijiiroId')}</Label>
            <Input
              id="edit-nijiiro-id"
              inputMode="numeric"
              value={nijiiroId}
              disabled={busy}
              onChange={(e) => setNijiiroId(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="edit-official-id">{t('db.officialId')}</Label>
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
            {busy ? t('messages.saving') : t('db.save')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
