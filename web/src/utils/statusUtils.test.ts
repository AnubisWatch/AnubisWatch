import { describe, expect, it } from 'vitest'
import {
  clusterStatusColors,
  getClusterStatusColor,
  getClusterStatusTextColor,
  getStatusColor,
  getStatusLabel,
  getStatusText,
  statusColors,
} from './statusUtils'

describe('statusUtils', () => {
  it('returns every configured display status token', () => {
    for (const [status, colors] of Object.entries(statusColors)) {
      expect(getStatusColor(status as keyof typeof statusColors)).toBe(colors.bg)
      expect(getStatusText(status as keyof typeof statusColors)).toBe(colors.text)
      expect(getStatusLabel(status as keyof typeof statusColors)).toBe(colors.label)
    }
  })

  it('defaults omitted display statuses to unknown', () => {
    expect(getStatusColor()).toBe(statusColors.unknown.bg)
    expect(getStatusText()).toBe(statusColors.unknown.text)
    expect(getStatusLabel()).toBe(statusColors.unknown.label)
  })

  it('returns configured cluster tokens and safe fallbacks', () => {
    for (const [status, colors] of Object.entries(clusterStatusColors)) {
      expect(getClusterStatusColor(status)).toBe(colors.bg)
      expect(getClusterStatusTextColor(status)).toBe(colors.text)
    }
    expect(getClusterStatusColor('new-status')).toBe('bg-gray-500')
    expect(getClusterStatusTextColor('new-status')).toBe('text-gray-400')
  })
})
