// What this site gives OurTaikoSiteKit's shared pages (see the kit's site.tsx): besides its
// card, search and own parts, the Nijiiro and official IDs on a chart's page.
import { useTranslation } from 'react-i18next'
import type { Site } from '@sitekit/site'
import type { Chart } from '@/api/types'
import { ChartCard } from '@/components/chart-card'
import { ChartSearch, chartOrders, useChartOrderLabels } from '@/components/chart-search'
import { ChartActivity } from './chart-activity'
import { EditMetadata } from './edit-metadata'
import { UploadPage } from './upload'

const choice = (value: string | null) => (value === 'yes' || value === 'no' ? value : '')

function NijiiroStat({ chart }: { chart: Chart }) {
  const { t } = useTranslation()
  return (
    <div>
      <dt>{t('db.nijiiroId')}</dt>
      <dd data-testid="detail-nijiiro">{chart.nijiiroId ?? '—'}</dd>
    </div>
  )
}

function IdentifierRecord({ chart }: { chart: Chart }) {
  const { t } = useTranslation()
  return (
    <>
      <dt>{t('db.nijiiroId')}</dt>
      <dd className="tabular-nums">{chart.nijiiroId ?? t('db.none')}</dd>
      <dt>{t('db.officialId')}</dt>
      <dd>{chart.officialId ?? t('db.none')}</dd>
    </>
  )
}

export const site: Site = {
  chartCard: (chart) => <ChartCard chart={chart as Chart} />,
  chartOrders,
  useChartOrderLabels,
  chartSearch: ({ filters, ...props }) => (
    <ChartSearch
      {...props}
      filters={{
        order: filters.order,
        category: filters.category,
        course: filters.course,
        level: filters.level,
        nijiiro: filters.nijiiro,
        files: filters.files,
      }}
    />
  ),
  readExtraFilters: (params) => ({
    nijiiro: choice(params.get('nijiiro')),
    files: choice(params.get('files')),
  }),
  ChartActivity: (props) => <ChartActivity {...props} chart={props.chart as Chart} />,
  EditMetadata: ({ chart, onSaved, ...props }) => (
    <EditMetadata {...props} chart={chart as Chart} onSaved={onSaved} />
  ),
  UploadPage,
  chartStats: (chart) => <NijiiroStat chart={chart as Chart} />,
  chartRecord: (chart) => <IdentifierRecord chart={chart as Chart} />,
}
