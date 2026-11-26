import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MCPServerSettings } from '../MCPServerSettings'

afterEach(cleanup)

it('shows write-only MCP keys and preserves their values when left blank', () => {
  const onChange = vi.fn()
  render(<MCPServerSettings servers={{
    github: {
      type: 'http',
      url: 'https://mcp.example.test',
      env_keys: ['GITHUB_TOKEN'],
      header_keys: ['Authorization'],
    },
  }} onChange={onChange} />)

  expect(screen.getByText('Env: GITHUB_TOKEN')).toBeInTheDocument()
  expect(screen.getByText('Headers: Authorization')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
  expect(screen.getByDisplayValue('GITHUB_TOKEN')).toBeInTheDocument()
  expect(screen.getByDisplayValue('Authorization')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))

  expect(onChange).toHaveBeenCalledWith({
    github: { type: 'http', url: 'https://mcp.example.test' },
  })
})
