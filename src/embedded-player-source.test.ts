import { afterEach, describe, expect, it, vi } from 'vitest'
import { resolvePlayerUrl } from './embedded-player-source'

const options = () => ({
  manifestUrl: 'https://d2mguycu233w0q.cloudfront.net/player-build.json',
  parentOrigin: 'https://database.ourtaiko.org',
  signal: new AbortController().signal,
})
afterEach(() => vi.unstubAllGlobals())

describe('player version discovery', () => {
  it('reads the latest CDN version each time without changing the website', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(Response.json({ path: '/player/aaaaaaaaaaaaaaaa/index.html' }))
      .mockResolvedValueOnce(Response.json({ path: '/player/bbbbbbbbbbbbbbbb/index.html' }))
    vi.stubGlobal('fetch', fetchMock)
    const first = new URL(await resolvePlayerUrl(options()))
    const second = new URL(await resolvePlayerUrl(options()))
    expect(first.origin).toBe('https://d2mguycu233w0q.cloudfront.net')
    expect(first.pathname).toBe('/player/aaaaaaaaaaaaaaaa/index.html')
    expect(second.pathname).toBe('/player/bbbbbbbbbbbbbbbb/index.html')
    expect(second.searchParams.get('parentOrigin')).toBe('https://database.ourtaiko.org')
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ cache: 'no-store', credentials: 'omit' })
  })

  it.each([null, {}, { path: '//evil.example/index.html' }, { path: '/player/../index.html' }])(
    'rejects invalid manifest %j',
    async (body) => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json(body)))
      await expect(resolvePlayerUrl(options())).rejects.toThrow('PLAYER_MANIFEST_INVALID')
    },
  )

  it('reports unavailable manifests', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 403 })))
    await expect(resolvePlayerUrl(options())).rejects.toThrow('PLAYER_MANIFEST_UNAVAILABLE')
  })
})
