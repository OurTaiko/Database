import { useTranslation } from 'react-i18next'
import { StarIcon } from '@phosphor-icons/react'
import type { Difficulty } from '@/tja'
import { groupCourses, groupLevel, groupName } from '@/courses'

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
          className="flex justify-between items-center gap-1 bg-[#f5f5f7] dark:bg-muted py-2 pr-3 pl-5 min-h-8 font-medium text-[#424245] dark:text-foreground text-xs [clip-path:polygon(0_0,100%_0,100%_100%,0_100%,8px_50%)]"
        >
          <span>{groupName(group)}</span>
          <span
            className="inline-flex items-center gap-0.5 tabular-nums"
            aria-label={group.sides
              .map((side) => t('common.stars', { count: side.level }))
              .filter((label, index, all) => all.indexOf(label) === index)
              .join(' / ')}
          >
            <StarIcon
              weight="fill"
              className="size-3 text-[#0071e3] dark:text-blue-400 motion-reduce:transform-none group-hover:scale-105 transition-transform motion-reduce:transition-none duration-500 ease-[cubic-bezier(0.25,0.1,0.25,1)]"
              aria-hidden="true"
            />
            {groupLevel(group)}
          </span>
        </li>
      ))}
    </ul>
  )
}
