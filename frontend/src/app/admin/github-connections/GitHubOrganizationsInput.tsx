'use client'

import { ClipboardEvent, KeyboardEvent, useState } from 'react'
import { X } from 'lucide-react'
import { parseGitHubOrganizations } from '@/utils/githubOrganizations'

interface GitHubOrganizationsInputProps {
  value: string[]
  onChange: (organizations: string[]) => void
}

export function GitHubOrganizationsInput({ value, onChange }: GitHubOrganizationsInputProps) {
  const [draft, setDraft] = useState('')

  const addOrganizations = (organizations: string[]) => {
    const additions = organizations.filter((organization, index) => !value.includes(organization) && organizations.indexOf(organization) === index)
    if (additions.length) onChange([...value, ...additions])
    setDraft('')
  }

  const commitDraft = () => addOrganizations(parseGitHubOrganizations(draft))

  const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.nativeEvent.isComposing) return
    if (event.key === ' ' || event.key === 'Enter' || event.key === ',') {
      event.preventDefault()
      commitDraft()
    } else if (event.key === 'Backspace' && !draft && value.length) {
      onChange(value.slice(0, -1))
    }
  }

  const handlePaste = (event: ClipboardEvent<HTMLInputElement>) => {
    const organizations = parseGitHubOrganizations(event.clipboardData.getData('text'))
    if (organizations.length > 1) {
      event.preventDefault()
      addOrganizations(organizations)
    }
  }

  const removeOrganization = (organization: string) => {
    onChange(value.filter(item => item !== organization))
  }

  return <div className="block text-sm dark:text-white">
    <label htmlFor="github-organizations-input">対象organization</label>
    <div className="mt-1 flex min-h-10 w-full flex-wrap items-center gap-2 rounded-md border border-gray-300 bg-white px-2 py-1.5 dark:border-gray-700 dark:bg-gray-900">
      {value.map(organization => <span key={organization} className="inline-flex items-center gap-1 rounded-full bg-blue-50 px-2 py-1 text-xs text-blue-700 dark:bg-blue-950 dark:text-blue-200">
        {organization}
        <button type="button" aria-label={`${organization}を削除`} onClick={() => removeOrganization(organization)} className="rounded-full hover:bg-blue-100 dark:hover:bg-blue-900"><X className="h-3 w-3" /></button>
      </span>)}
      <input
        id="github-organizations-input"
        aria-label="対象organizationを追加"
        className="min-w-36 flex-1 border-0 bg-transparent px-1 py-0.5 text-sm outline-none dark:text-white"
        placeholder={value.length ? '' : 'example-org'}
        value={draft}
        onChange={event => setDraft(event.target.value)}
        onKeyDown={handleKeyDown}
        onPaste={handlePaste}
        onBlur={commitDraft}
      />
    </div>
    <span className="mt-1 block text-xs text-gray-500">organization名を入力し、SpaceまたはEnterで追加します。repository ownerが一致するセッションでは、このconnectionのtokenを自動選択します。</span>
  </div>
}
