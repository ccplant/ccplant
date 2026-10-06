import { describe, expect, it } from 'vitest'
import { formatCompactNumber, formatDuration } from './format'

describe('usage formatting', () => {
  it('formats durations without hiding sub-minute activity', () => {
    expect(formatDuration(0)).toBe('0m')
    expect(formatDuration(42)).toBe('<1m')
    expect(formatDuration(3_900)).toBe('1h 5m')
  })

  it('uses a compact representation for large counts', () => {
    expect(formatCompactNumber(12)).toBe('12')
    expect(formatCompactNumber(12_000)).not.toBe('12,000')
  })
})
