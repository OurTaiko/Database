import { useTranslation } from 'react-i18next'
import { StarIcon } from '@phosphor-icons/react'
import type { Difficulty } from '@/tja'
import { groupCourses, groupLevel, groupName } from '@sitekit/courses'
import { cn } from '@sitekit/lib/utils'

// Fixed course colours, dark text on both themes (AA on every swatch).
export const difficultyColors = {
  Easy: 'bg-[#f9a15a]',
  Normal: 'bg-[#8fd16a]',
  Hard: 'bg-[#6cc7d8]',
  Oni: 'bg-[#f77fab]',
  Edit: 'bg-[#b99ae8]',
}

// One bookmark per course: its single, 1P and 2P charts are one entry, so a
// song has at most five and they fit the fixed card height.
export function DifficultyBadges({ difficulties }: { difficulties: Difficulty[] }) {
  const { t } = useTranslation()
  return (
    <ul
      aria-label={t('messages.chartDifficulty')}
      className="flex flex-col self-start gap-1.5 w-max min-w-24 shrink-0 whitespace-nowrap"
    >
      {groupCourses(difficulties).map((group) => (
        <li
          key={group.base}
          className={cn(
            'inline-flex justify-between items-center gap-1.5 px-2.5 border-[1.5px] border-[#382d2a] rounded-[7px] h-7 font-bold text-[#382d2a] text-xs whitespace-nowrap',
            difficultyColors[group.base as keyof typeof difficultyColors] ?? 'bg-muted',
          )}
        >
          <span>{groupName(group)}</span>
          <span
            className="inline-flex items-center gap-0.5 tabular-nums"
            aria-label={group.sides
              .map((side) => t('common.stars', { count: side.level }))
              .filter((label, index, all) => all.indexOf(label) === index)
              .join(' / ')}
          >
            <StarIcon weight="fill" className="size-3" aria-hidden="true" />
            {groupLevel(group)}
          </span>
        </li>
      ))}
    </ul>
  )
}
