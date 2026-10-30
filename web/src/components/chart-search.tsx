import { useTranslation } from 'react-i18next'
import type { ComponentProps } from 'react'
import { ChoiceSelect } from '@sitekit/components/choice-select'
import { ChartSearch as SharedChartSearch } from '@sitekit/components/chart-search'
import type { ChartFilters as SharedChartFilters } from '@sitekit/components/chart-search'

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
/** The shared filters, and whether a chart has a Nijiiro ID or files. */
export type ChartFilters = SharedChartFilters & { nijiiro: string; files: string }
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

/** OurTaikoSiteKit's chart search with this site's orders and its Nijiiro and files filters. */
export function ChartSearch({
  filters,
  onChange,
  ...props
}: Omit<ComponentProps<typeof SharedChartSearch>, 'orders' | 'orderLabels' | 'filters'> & {
  filters: ChartFilters
}) {
  const { t } = useTranslation()
  const labels = useChartOrderLabels()
  return (
    <SharedChartSearch
      {...props}
      filters={filters}
      onChange={onChange}
      orders={chartOrders}
      orderLabels={labels}
      placeholder={t('db.searchPlaceholder')}
    >
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
          { value: 'no', label: t('messages.noFiles') },
        ]}
        onValueChange={(value) => onChange({ files: value })}
      />
    </SharedChartSearch>
  )
}
