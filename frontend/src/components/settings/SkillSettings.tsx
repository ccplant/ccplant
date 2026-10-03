'use client'

import { useState } from 'react'

interface SkillSettingsProps {
  skills?: string[]
  onChange: (skills: string[]) => void
}

export function SkillSettings({ skills = [], onChange }: SkillSettingsProps) {
  const [source, setSource] = useState('')
  const [skillName, setSkillName] = useState('')
  const normalized = source.trim()

  const add = () => {
    const configured = skillName.trim() ? `${normalized} --skill ${skillName.trim()}` : normalized
    if (!normalized || skills.includes(configured)) return
    onChange([...skills, configured])
    setSource('')
    setSkillName('')
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
      <div className="grid gap-2 border-t border-gray-200 pt-4 dark:border-gray-700 sm:grid-cols-[minmax(0,1fr)_minmax(0,14rem)_auto]">
        <input value={source} onChange={(event) => setSource(event.target.value)}
          onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); add() } }}
          placeholder="owner/repository or https://skills.sh/..."
          className="flex-1 rounded-md border border-gray-300 bg-white px-3 py-2 text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-white" />
        <input value={skillName} onChange={(event) => setSkillName(event.target.value)}
          onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); add() } }}
          placeholder="Skill name (optional)"
          className="rounded-md border border-gray-300 bg-white px-3 py-2 text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-white" />
        <button type="button" onClick={add} disabled={!normalized}
          className="rounded-md bg-blue-600 px-4 py-2 text-white disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600">
          + Add
        </button>
      </div>
      <p className="text-xs text-gray-500 dark:text-gray-400">
        Optionally select one skill from the package. Leaving the skill name empty installs all skills. The resolved agent type selects either Claude Code or Codex.
      </p>
    </div>
  )
}
