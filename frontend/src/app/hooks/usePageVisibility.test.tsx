import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useBackgroundAwareInterval } from './usePageVisibility'

describe('useBackgroundAwareInterval', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('does not overlap callbacks when a request takes longer than the interval', async () => {
    vi.useFakeTimers()
    let finishRequest: (() => void) | undefined
    const callback = vi.fn(() => new Promise<void>((resolve) => {
      finishRequest = resolve
    }))

    const { result } = renderHook(() => useBackgroundAwareInterval(callback, 1000, true))

    act(() => result.current.start())
    await act(async () => { await Promise.resolve() })
    expect(callback).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(callback).toHaveBeenCalledTimes(1)

    await act(async () => {
      finishRequest?.()
      await Promise.resolve()
    })
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(callback).toHaveBeenCalledTimes(2)
  })

  it('stays stopped when stop is called while the page is hidden', () => {
    vi.useFakeTimers()
    const callback = vi.fn()
    const { result } = renderHook(() => useBackgroundAwareInterval(callback, 1000, false))

    act(() => result.current.start())
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    act(() => document.dispatchEvent(new Event('visibilitychange')))
    act(() => result.current.stop())
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    act(() => document.dispatchEvent(new Event('visibilitychange')))
    act(() => vi.advanceTimersByTime(2000))

    expect(callback).not.toHaveBeenCalled()
  })
})
