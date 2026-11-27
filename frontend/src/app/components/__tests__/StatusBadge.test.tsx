import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import StatusBadge from '../StatusBadge'

describe('StatusBadge', () => {
  it('renders suspended sessions with the violet suspended style', () => {
    render(<StatusBadge status="suspended" />)

    const badge = screen.getByText('Suspended').closest('span')
    expect(badge).toHaveClass('bg-violet-100', 'text-violet-800', 'border-violet-300')
  })

  it.each([
    ['terminating', '削除中'],
    ['terminated', '削除済み'],
  ] as const)('renders %s as a non-error lifecycle state', (status, label) => {
    render(<StatusBadge status={status} />)
    expect(screen.getByText(label)).toBeInTheDocument()
  })
})
