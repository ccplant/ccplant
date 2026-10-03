'use client'

import { useState } from 'react'

interface SkillSettingsProps {
  skills?: string[]
  onChange: (skills: string[]) => void
}

export function SkillSettings({ skills = [], onChange }: SkillSettingsProps) {
  const [source, setSource] = useState('')
  const [skillName, setSkillName] = useState('')
  const [allSkills, setAllSkills] = useState(false)
  const normalized = source.trim()
  const normalizedSkillName = skillName.trim()
  const canAdd = Boolean(normalized && (allSkills || normalizedSkillName))

  const add = () => {
    if (!canAdd) return
    const configured = `${normalized} --skill ${allSkills ? '*' : normalizedSkillName}`
    if (skills.includes(configured)) return
    onChange([...skills, configured])
    setSource('')
    setSkillName('')
    setAllSkills(false)
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
          disabled={allSkills}
          placeholder={allSkills ? "All skills selected" : "Skill name (required)"}
          className="rounded-md border border-gray-300 bg-white px-3 py-2 text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-white" />
        <button type="button" onClick={add} disabled={!canAdd}
          className="rounded-md bg-blue-600 px-4 py-2 text-white disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600">
          + Add
        </button>
        <label className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300 sm:col-start-2">
          <input type="checkbox" checked={allSkills}
            onChange={(event) => setAllSkills(event.target.checked)} />
          Install all skills from this package
        </label>
      </div>
      <p className="text-xs text-gray-500 dark:text-gray-400">
        A skill name is required. Select “Install all skills” explicitly to use <code>--skill &apos;*&apos;</code>. The resolved agent type selects either Claude Code or Codex.
      </p>
    </div>
  )
}
