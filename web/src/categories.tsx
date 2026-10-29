import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import { Badge } from '@/components/ui/badge'
import { ChoiceSelect } from '@/components/choice-select'
import { createContext, useContext, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'
import { api } from './api/client'
import { categoryColor } from './category-colors'
import type { Category, CategoryList } from './api/types'
import { i18n } from './i18n'

const CategoryContext = createContext<{
  items: Category[] | null
  chartCount: number
  error: string
  retry: () => void
}>({ items: null, chartCount: 0, error: '', retry: () => {} })

export function CategoryProvider({ children }: { children: ReactNode }) {
  const [data, setData] = useState<CategoryList | null>(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    api<CategoryList>(endpoints.categories, { signal: controller.signal })
      .then((list) => {
        if (!controller.signal.aborted) setData(list)
      })
      .catch((e: Error) => {
        if (!controller.signal.aborted) setError(e.message)
      })
    return () => controller.abort()
  }, [attempt])
  const retry = () => {
    setError('')
    setAttempt((n) => n + 1)
  }
  return (
    <CategoryContext.Provider
      value={{ items: data?.items ?? null, chartCount: data?.chartCount ?? 0, error, retry }}
    >
      {children}
    </CategoryContext.Provider>
  )
}
export function useCategories() {
  return useContext(CategoryContext)
}
// Category names are translated here by ID; the API's title is the fallback.
export function categoryName(category: Pick<Category, 'id' | 'title'>): string {
  const key = `db.categories.${category.id}`
  return i18n.exists(key) ? i18n.t(key as 'db.categories.namco') : category.title
}
export function CategoryLabels({ ids = [], children }: { ids?: string[]; children?: ReactNode }) {
  const { t } = useTranslation()
  const { items } = useContext(CategoryContext)
  if (ids.length === 0 && !children) return null
  const uniqueIds = [...new Set(ids)]
  return (
    <ul
      className="flex min-w-0 flex-wrap items-center gap-2"
      aria-label={children ? t('messages.chartInformation') : t('messages.categories')}
    >
      {children}
      {uniqueIds.map((id) => (
        <li key={id}>
          <Badge
            variant="outline"
            className={cn(
              categoryColor(id) && 'border-[1.5px] border-[#382d2a] font-bold text-[#382d2a]',
            )}
            style={{ background: categoryColor(id) }}
          >
            {categoryName(items?.find((category) => category.id === id) ?? { id, title: id })}
          </Badge>
        </li>
      ))}
    </ul>
  )
}
export function CategoryPicker({
  value,
  onChange,
  disabled = false,
}: {
  value: string[]
  onChange: (ids: string[]) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const { items, error, retry } = useContext(CategoryContext)
  return (
    <fieldset
      className="min-w-0 space-y-3 [&>legend]:mb-2 [&>legend]:text-sm [&>legend]:font-medium"
      disabled={disabled}
    >
      <legend>{t('messages.chartCategories')}</legend>
      <p className="text-sm text-muted-foreground">{t('db.categoriesHint')}</p>
      {error ? (
        <div role="alert" className="flex flex-wrap items-center gap-3 text-sm text-destructive">
          <span>{t('messages.categoryLoadError', { message: error })}</span>
          <Button type="button" variant="outline" size="sm" onClick={retry}>
            {t('messages.reloadCategories')}
          </Button>
        </div>
      ) : items === null ? (
        <p role="status" className="text-sm text-muted-foreground">
          {t('messages.loadingCategories')}
        </p>
      ) : (
        <div className="flex flex-wrap gap-x-6 gap-y-4">
          {items.map((category) => (
            <Label key={category.id} className="inline-flex items-center gap-2 text-sm">
              <Checkbox
                disabled={disabled}
                checked={value.includes(category.id)}
                onCheckedChange={(checked) => {
                  onChange(
                    checked
                      ? [...value, category.id].sort()
                      : value.filter((id) => id !== category.id),
                  )
                }}
              />
              <span>{categoryName(category)}</span>
            </Label>
          ))}
        </div>
      )}
    </fieldset>
  )
}

export function CategorySelect({
  label,
  value,
  onChange,
  disabled = false,
}: {
  label: string
  value: string
  onChange: (id: string) => void
  disabled?: boolean
}) {
  const { items } = useContext(CategoryContext)
  return (
    <ChoiceSelect
      label={label}
      value={value}
      onValueChange={onChange}
      disabled={disabled || !items}
      items={(items ?? []).map((category) => ({
        value: category.id,
        label: categoryName(category),
      }))}
    />
  )
}
