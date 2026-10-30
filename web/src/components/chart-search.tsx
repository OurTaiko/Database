import { useTranslation } from 'react-i18next'
import { SortDescendingIcon } from '@phosphor-icons/react'
import { ChoiceSelect } from '@sitekit/components/choice-select'
import { SearchInput } from '@sitekit/components/search-input'
import { categoryName, useCategories } from '@/categories'
import { cn } from '@sitekit/lib/utils'
import { categoryColor } from '@sitekit/category-colors'

export const chartOrders = [
  'hot',
  'top',
  'comments',
  'nijiiro',
  'title',
  'level',
  'bpm',
  'updated',
] as const
export type ChartOrder = (typeof chartOrders)[number]
export const filterCourses = ['Easy', 'Normal', 'Hard', 'Oni', 'Edit'] as const
export type ChartFilters = {
  order: '' | ChartOrder
  category: string
  course: string
  level: string
  nijiiro: string
  files: string
}
export function useChartOrderLabels(): Record<'' | ChartOrder, string> {
  const { t } = useTranslation()
  return {
    '': t('db.orderLatest'),
    hot: t('interactions.orderHot'),
    top: t('interactions.orderTop'),
    comments: t('interactions.orderComments'),
    nijiiro: t('db.orderNijiiro'),
    title: t('db.orderTitle'),
    level: t('db.orderLevel'),
    bpm: t('db.orderBpm'),
    updated: t('db.orderUpdated'),
  }
}
const chipClassName =
  'inline-flex h-8 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-3 text-sm transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring'
export function ChartSearch({
  query,
  filters,
  onSearch,
  onPendingChange,
  onChange,
  counts = true,
}: {
  query: string
  filters: ChartFilters
  onSearch: (query: string) => void
  onPendingChange: (pending: boolean) => void
  onChange: (values: Partial<ChartFilters>) => void
  /** The chips' chart counts are the whole catalogue's: off for a user's own list. */
  counts?: boolean
}) {
  const { t } = useTranslation()
  const labels = useChartOrderLabels()
  const { items: categories, chartCount } = useCategories()
  const courseLabels: Record<string, string> = {
    Easy: t('messages.easy'),
    Normal: t('messages.normal'),
    Hard: t('messages.hard'),
    Oni: t('messages.oni'),
    Edit: t('messages.uraOni'),
  }
  return (
    <div
      role="search"
      aria-label={t('messages.searchAndFilterCharts')}
      className="flex flex-col gap-3"
    >
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <SearchInput
          value={query}
          onSearch={onSearch}
          onPendingChange={onPendingChange}
          placeholder={t('db.searchPlaceholder')}
        />
        <div className="shrink-0 [&_[data-slot=select-trigger]]:w-full sm:[&_[data-slot=select-trigger]]:w-auto">
          <ChoiceSelect
            label={t('messages.searchOrder')}
            value={filters.order}
            prefix={
              <SortDescendingIcon aria-hidden="true" className="size-4 text-muted-foreground" />
            }
            items={(['', ...chartOrders] as const).map((value) => ({
              value,
              label: labels[value],
            }))}
            onValueChange={(value) => onChange({ order: value as ChartFilters['order'] })}
          />
        </div>
      </div>
      <ul
        aria-label={t('messages.categories')}
        className="-mx-1 flex items-center gap-2 overflow-x-auto px-1 py-1"
      >
        {[{ id: '', title: t('db.allCategories'), chartCount }, ...(categories ?? [])].map(
          (category) => {
            const active = filters.category === category.id
            return (
              <li key={category.id}>
                <button
                  type="button"
                  aria-pressed={active}
                  data-category={category.id}
                  className={cn(
                    chipClassName,
                    active
                      ? 'border-foreground bg-foreground text-background'
                      : 'bg-background/70 text-foreground hover:bg-muted',
                  )}
                  onClick={() => onChange({ category: category.id })}
                >
                  {categoryColor(category.id) && (
                    <span
                      aria-hidden="true"
                      className="size-2.5 shrink-0 rounded-full border-[1.5px] border-[#382d2a]"
                      style={{ background: categoryColor(category.id) }}
                    />
                  )}
                  {category.id ? categoryName(category) : category.title}
                  {counts && (
                    <span
                      className={cn('text-xs tabular-nums', !active && 'text-muted-foreground')}
                    >
                      {category.chartCount}
                    </span>
                  )}
                </button>
              </li>
            )
          },
        )}
      </ul>
      <div className="flex flex-wrap items-center gap-2">
        <ChoiceSelect
          label={t('db.filterCourse')}
          value={filters.course}
          items={[
            { value: '', label: t('db.anyCourse') },
            ...filterCourses.map((value) => ({ value, label: courseLabels[value] })),
          ]}
          onValueChange={(value) => onChange({ course: value })}
        />
        <ChoiceSelect
          label={t('db.filterLevel')}
          value={filters.level}
          items={[
            { value: '', label: t('db.anyLevel') },
            ...Array.from({ length: 10 }, (_, index) => ({
              value: String(10 - index),
              label: `★ ${10 - index}`,
            })),
          ]}
          onValueChange={(value) => onChange({ level: value })}
        />
        <ChoiceSelect
          label={t('db.filterNijiiro')}
          value={filters.nijiiro}
          items={[
            { value: '', label: t('db.nijiiroAny') },
            { value: 'yes', label: t('db.nijiiroYes') },
            { value: 'no', label: t('db.nijiiroNo') },
          ]}
          onValueChange={(value) => onChange({ nijiiro: value })}
        />
        <ChoiceSelect
          label={t('db.filterFiles')}
          value={filters.files}
          items={[
            { value: '', label: t('db.filesAny') },
            { value: 'yes', label: t('db.filesYes') },
            { value: 'no', label: t('db.noFiles') },
          ]}
          onValueChange={(value) => onChange({ files: value })}
        />
      </div>
    </div>
  )
}
