import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProfileConnectionFields from '../ProfileConnectionFields'

afterEach(cleanup)

describe('ProfileConnectionFields', () => {
  it('selects and clears web search independently of the API key', () => {
    const onChange = vi.fn()
    render(<ProfileConnectionFields agent="codex" value={{ mode: 'openai_compatible', base_url: 'https://gateway.example/v1', authentication: 'api_key', has_api_key: true, web_search_enabled: false }} onChange={onChange} />)
    expect(screen.getByLabelText(/Web search tool/)).toHaveValue('false')
    fireEvent.change(screen.getByLabelText(/Web search tool/), { target: { value: 'true' } })
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ web_search_enabled: true, has_api_key: true }))
    fireEvent.change(screen.getByLabelText(/Web search tool/), { target: { value: '' } })
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ web_search_enabled: null }))
  })

  it.each(['codex', 'claude'] as const)('edits and previews the %s endpoint path', agent => {
    const onChange = vi.fn()
    render(<ProfileConnectionFields agent={agent} value={{ mode: agent === 'codex' ? 'openai_compatible' : 'anthropic_compatible', base_url: 'https://gateway.example/prefix/', endpoint_path: '/custom/generate', authentication: 'none' }} onChange={onChange} />)
    expect(screen.getByText('送信先: https://gateway.example/prefix/custom/generate')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText(/API パス/), { target: { value: '/other' } })
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ endpoint_path: '/other' }))
  })
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
