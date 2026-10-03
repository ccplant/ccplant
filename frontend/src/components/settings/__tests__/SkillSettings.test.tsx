import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { SkillSettings } from '../SkillSettings'

afterEach(cleanup)

describe('SkillSettings', () => {
  it('requires a skill name before adding a package', () => {
    const onChange = vi.fn()
    render(<SkillSettings onChange={onChange} />)

    fireEvent.change(screen.getByPlaceholderText('owner/repository or https://skills.sh/...'), {
      target: { value: 'https://github.com/mattpocock/skills' },
    })

    expect(screen.getByRole('button', { name: '+ Add' })).toBeDisabled()
    expect(onChange).not.toHaveBeenCalled()
  })

  it('stores an explicitly selected skill', () => {
    const onChange = vi.fn()
    render(<SkillSettings onChange={onChange} />)

    fireEvent.change(screen.getByPlaceholderText('owner/repository or https://skills.sh/...'), {
      target: { value: 'https://github.com/mattpocock/skills' },
    })
    fireEvent.change(screen.getByPlaceholderText('Skill name (required)'), {
      target: { value: 'grill-me' },
    })
    fireEvent.click(screen.getByRole('button', { name: '+ Add' }))

    expect(onChange).toHaveBeenCalledWith([
      'https://github.com/mattpocock/skills --skill grill-me',
    ])
  })

  it('stores the wildcard only after all skills is explicitly selected', () => {
    const onChange = vi.fn()
    render(<SkillSettings onChange={onChange} />)

    fireEvent.change(screen.getByPlaceholderText('owner/repository or https://skills.sh/...'), {
      target: { value: 'owner/repository' },
    })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Install all skills from this package' }))
    fireEvent.click(screen.getByRole('button', { name: '+ Add' }))

    expect(onChange).toHaveBeenCalledWith(['owner/repository --skill *'])
  })
})
