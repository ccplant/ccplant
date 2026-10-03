'use client'

import { useState } from 'react'

interface SkillSettingsProps {
  skills?: string[]
  onChange: (skills: string[]) => void
}

export function SkillSettings({ skills = [], onChange }: SkillSettingsProps) {
  const [source, setSource] = useState('')
  const normalized = source.trim()

  const add = () => {
    if (!normalized || skills.includes(normalized)) return
    onChange([...skills, normalized])
    setSource('')
  }

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        {skills.length === 0 ? (
          <p className="rounded-lg bg-gray-50 p-4 text-sm text-gray-500 dark:bg-gray-800/50 dark:text-gray-400">
            No skills.sh packages configured
          </p>
        ) : skills.map((skill) => (
          <div key={skill} className="flex items-center justify-between rounded-lg border border-gray-200 px-4 py-3 dark:border-gray-700">
            <code className="text-sm text-gray-900 dark:text-white">{skill}</code>
            <button type="button" onClick={() => onChange(skills.filter(item => item !== skill))}
              className="text-sm font-medium text-red-600 hover:text-red-800 dark:text-red-400">
              Delete
            </button>
          </div>
        ))}
      </div>
      <div className="flex gap-2 border-t border-gray-200 pt-4 dark:border-gray-700">
        <input value={source} onChange={(event) => setSource(event.target.value)}
          onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); add() } }}
          placeholder="owner/repository or https://skills.sh/..."
          className="flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-white" />
        <button type="button" onClick={add} disabled={!normalized}
          className="rounded-md bg-blue-600 px-4 py-2 text-white disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600">
          + Add
        </button>
      </div>
      <p className="text-xs text-gray-500 dark:text-gray-400">
        The session&apos;s resolved agent type selects either Claude Code or Codex. Packages are never installed for every agent at once.
      </p>
    </div>
  )
}
