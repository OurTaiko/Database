import { beforeEach, describe, expect, it } from 'vitest'
import { i18n } from './i18n'
import { blockKey, courseNames, groupCourses, supportsChart } from './courses'
import { defaultDifficulty } from './chart-difficulty'
import { parsePreviewTja } from './preview-tja'
import type { Difficulty } from './tja'

beforeEach(async () => {
  await i18n.changeLanguage('zh-Hans')
})

describe('detail difficulty selection', () => {
  const courses = ['Easy', 'Normal', 'Hard', 'Edit', 'Oni']
  it('defaults to Oni and follows the requested fallback order', () => {
    while (courses.length) {
      const difficulties: Difficulty[] = courses.map((course) => ({
        course,
        side: 'single',
        maker: '',
        level: 5,
      }))
      expect(defaultDifficulty(difficulties)).toBe(courses.at(-1))
      courses.pop()
    }
    expect(defaultDifficulty([])).toBe('')
    expect(defaultDifficulty([{ course: 'Tower', side: 'single', level: 5, maker: '' }])).toBe('')
    expect(Object.keys(courseNames)).toHaveLength(5)
    expect(supportsChart({ difficulties: [{ course: 'Oni' }, { course: 'Dan' }] })).toBe(false)
    expect(supportsChart({ difficulties: [{ course: 'Oni' }, { course: 'Edit' }] })).toBe(true)
    // A song registered without files has no blocks yet; a chart with files must.
    expect(supportsChart({ difficulties: [], hasFiles: false })).toBe(true)
    expect(supportsChart({ difficulties: [], hasFiles: true })).toBe(false)
    expect(supportsChart({ difficulties: [{ course: 'Dan' }], hasFiles: false })).toBe(false)
  })
  it('names blocks as the preview parser does and prefers the single chart', () => {
    const p1: Difficulty = { course: 'Oni', side: 'p1', level: 9, maker: '' }
    const p2: Difficulty = { course: 'Oni', side: 'p2', level: 10, maker: '' }
    const single: Difficulty = { course: 'Oni', side: 'single', level: 8, maker: '' }
    expect([single, p1, p2].map(blockKey)).toEqual(['Oni', 'Oni_1p', 'Oni_2p'])
    expect(defaultDifficulty([p2, p1])).toBe('Oni_1p')
    expect(defaultDifficulty([p2, p1, single])).toBe('Oni')
    const [group] = groupCourses([p2, single, p1])
    expect(group.double).toBe(false)
    expect(group.sides.map((d) => d.side)).toEqual(['single', 'p1', 'p2'])
    expect(groupCourses([p2, p1])[0].double).toBe(true)
  })
})

describe('TJA renderer input adapter', () => {
  it('isolates Single and P1/P2 blocks of a numeric course without losing metadata', () => {
    const text = `TITLE:English name
BPM:150
COURSE:3
LEVEL:8
STYLE:Single
#START
1111,
#END
COURSE:3
LEVEL:10
STYLE:Double
#START P1
2222,
#END
#START P2
3333,
#END
COURSE:Hard
LEVEL:6
#START
1212,
#END`
    const parsed = parsePreviewTja(text)
    expect(Object.keys(parsed)).toEqual(['Oni', 'Oni_1p', 'Oni_2p', 'Hard'])
    expect(Object.values(parsed).map((c) => c.level)).toEqual([8, 10, 10, 6])
    expect(Object.values(parsed).map((c) => c.bars[0].join(''))).toEqual([
      '1111',
      '2222',
      '3333',
      '1212',
    ])
    expect(Object.values(parsed).every((c) => c.title === 'English name' && c.bpm === 150)).toBe(
      true,
    )
  })
  it('accepts BOM, CRLF, comments, lowercase headers and the implicit Oni course', () => {
    const parsed = parsePreviewTja(
      '\uFEFFtitle:Song\r\nbpm:180\r\nlevel:7\r\n#start // comment\r\n1000,\r\n#end\r\n',
    )
    expect(parsed.Oni.title).toBe('Song')
    expect(parsed.Oni.level).toBe(7)
    expect(parsed.Oni.bars[0].join('')).toBe('1000')
  })
  it('preserves selectable branch charts', () => {
    const parsed = parsePreviewTja(`TITLE:Branch
BPM:120
COURSE:Oni
#START
1000,
#BRANCHSTART p,50,80
#N
1111,
#E
2222,
#M
1212,
#BRANCHEND
#END`)
    expect(Object.keys(parsed.Oni.branches ?? {})).toEqual(['normal', 'expert', 'master'])
    expect(parsed.Oni.branches?.master?.bars.some((bar) => bar.join('') === '1212')).toBe(true)
  })
  it.each(['Tower', 'Dan', '5', '6', 'tower', 'dAn'])('rejects %s in preview input', (course) => {
    expect(() =>
      parsePreviewTja(
        `TITLE:Song\nBPM:120\nCOURSE:Oni\n#START\n1,\n#END\nCOURSE:${course}\n#START\n1,\n#END`,
      ),
    ).toThrow('仅支持')
  })
  it('reports incomplete data instead of showing another chart', () => {
    expect(() => parsePreviewTja('TITLE:Empty')).toThrow()
    expect(() => parsePreviewTja('COURSE:Oni\n#START\n1,')).toThrow()
  })
})

it('keeps side identities when the TJA starts with P2', () => {
  const parsed = parsePreviewTja(
    'TITLE:D\nBPM:120\nCOURSE:Oni\nLEVEL:10\n#START P2\n2222,\n#END\n#START P1\n1111,\n#END',
  )
  expect(parsed.Oni_1p.bars[0].join('')).toBe('1111')
  expect(parsed.Oni_2p.bars[0].join('')).toBe('2222')
})
