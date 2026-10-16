import { beforeEach, describe, expect, it, vi } from 'vitest'

const get = vi.fn()
vi.mock('./client', () => ({ api: { get: (path: string) => get(path) } }))

import { fetchAllArrayPages, fetchAllPages } from './pagination'

function query(path: string) {
  const params = new URL(path, 'http://x').searchParams
  return { offset: Number(params.get('offset')), limit: Number(params.get('limit')) }
}

describe('pagination helpers', () => {
  beforeEach(() => get.mockReset())

  // The Souls page used to stop at the API's default page of 20.
  it('fetchAllPages follows next_offset until has_more is false', async () => {
    const all = Array.from({ length: 250 }, (_, i) => ({ id: `s${i}` }))
    get.mockImplementation(async (path: string) => {
      const { offset, limit } = query(path)
      const data = all.slice(offset, offset + limit)
      const hasMore = offset + data.length < all.length
      return { data, pagination: { total: all.length, offset, limit, has_more: hasMore, next_offset: hasMore ? offset + data.length : undefined } }
    })
    const result = await fetchAllPages<{ id: string }>('/souls')
    expect(result.data).toHaveLength(250)
    expect(result.pagination?.has_more).toBe(false)
    expect(get).toHaveBeenCalledTimes(3)
  })

  it('fetchAllPages rejects a non-advancing next_offset', async () => {
    get.mockResolvedValue({ data: [{ id: 'a' }], pagination: { total: 9, offset: 0, limit: 100, has_more: true, next_offset: 0 } })
    await expect(fetchAllPages('/souls')).rejects.toThrow(/next_offset must advance/)
  })

  // Journeys used to show at most 20 entries with no indication more existed.
  it('fetchAllArrayPages walks bare-array pages until a short page', async () => {
    const all = Array.from({ length: 150 }, (_, i) => ({ id: `j${i}` }))
    get.mockImplementation(async (path: string) => {
      const { offset, limit } = query(path)
      return all.slice(offset, offset + limit)
    })
    const rows = await fetchAllArrayPages<{ id: string }>('/journeys')
    expect(rows).toHaveLength(150)
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('fetchAllArrayPages treats null as an empty list', async () => {
    get.mockResolvedValue(null)
    await expect(fetchAllArrayPages('/journeys')).resolves.toEqual([])
  })
})
