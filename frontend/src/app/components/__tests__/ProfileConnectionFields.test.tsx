import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProfileConnectionFields from '../ProfileConnectionFields'

afterEach(cleanup)

describe('ProfileConnectionFields', () => {
  it('updates Codex model metadata for a session profile', () => {
    const onChange = vi.fn()
    render(<ProfileConnectionFields agent="codex" value={{ mode: 'openai_compatible', base_url: 'https://gateway.example/v1', model: 'custom-model', authentication: 'none' }} onChange={onChange} />)

    fireEvent.click(screen.getByText('モデルメタデータ'))
    fireEvent.change(screen.getByLabelText('コンテキスト長'), { target: { value: '128000' } })
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ context_window: 128000 }))

    fireEvent.change(screen.getByLabelText('自動圧縮開始トークン数'), { target: { value: '64000' } })
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ auto_compact_token_limit: 64000 }))

    fireEvent.change(screen.getByLabelText('Reasoning summaries'), { target: { value: 'true' } })
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ supports_reasoning_summaries: true }))
  })

  it('does not show Codex metadata for Claude Code', () => {
    render(<ProfileConnectionFields agent="claude" value={{ mode: 'anthropic_compatible', base_url: 'https://gateway.example', model: 'claude-model', authentication: 'api_key' }} onChange={vi.fn()} />)
    expect(screen.queryByText('モデルメタデータ')).not.toBeInTheDocument()
  })
})
