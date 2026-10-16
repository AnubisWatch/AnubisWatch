import { api } from './client'
import type { ApiResponse } from './client'

const listPageLimit = 100
const maxListPages = 1000

// Fetch every page of a paginated ({data, pagination}) list endpoint, for
// screens that render a complete client-side collection. A non-advancing or
// missing next_offset is rejected so a malformed response can never trap the
// dashboard in an infinite pagination loop.
export async function fetchAllPages<T>(path: string): Promise<ApiResponse<T[]>> {
  let offset = 0
  let firstResponse: ApiResponse<T[]> | null = null
  const data: T[] = []

  for (let page = 0; page < maxListPages; page += 1) {
    const response = await api.get<ApiResponse<T[]>>(`${path}?offset=${offset}&limit=${listPageLimit}`)
    firstResponse ??= response
    data.push(...(response.data || []))

    if (!response.pagination?.has_more) {
      if (page === 0) return response
      return {
        ...firstResponse,
        data,
        pagination: firstResponse.pagination
          ? { ...firstResponse.pagination, offset: 0, limit: data.length, has_more: false }
          : undefined,
      }
    }

    const nextOffset = response.pagination.next_offset
    if (typeof nextOffset !== 'number' || nextOffset <= offset) {
      throw new Error(`Invalid pagination from ${path}: next_offset must advance`)
    }
    offset = nextOffset
  }

  throw new Error(`Pagination limit exceeded for ${path}`)
}

// Fetch every page of a list endpoint that returns a bare array. A page
// shorter than the requested limit is the last one.
export async function fetchAllArrayPages<T>(path: string): Promise<T[]> {
  const data: T[] = []
  for (let page = 0; page < maxListPages; page += 1) {
    const rows = (await api.get<T[] | null>(`${path}?offset=${data.length}&limit=${listPageLimit}`)) ?? []
    data.push(...rows)
    if (rows.length < listPageLimit) return data
  }
  throw new Error(`Pagination limit exceeded for ${path}`)
}
