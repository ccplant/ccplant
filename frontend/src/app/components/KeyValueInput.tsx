'use client'

export interface KeyValuePair {
  key: string
  value: string
}

interface KeyValueInputProps {
  pairs: KeyValuePair[]
  onChange: (pairs: KeyValuePair[]) => void
  disabled?: boolean
  helpText?: string
}

export default function KeyValueInput({ pairs, onChange, disabled = false, helpText }: KeyValueInputProps) {
  const removePair = (index: number) => {
    onChange(pairs.length === 1 ? [{ key: '', value: '' }] : pairs.filter((_, i) => i !== index))
  }

  const updatePair = (index: number, field: 'key' | 'value', value: string) => {
    const next = [...pairs]
    next[index] = { ...next[index], [field]: value }
    onChange(next)
  }

  return (
    <div>
      <div className="space-y-2">
        {pairs.map((pair, index) => (
          <div key={index} className="flex items-center gap-2">
            <input type="text" value={pair.key} onChange={(event) => updatePair(index, 'key', event.target.value)} placeholder="key" disabled={disabled} className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono" />
            <span className="text-gray-400 dark:text-gray-500 text-sm">=</span>
            <input type="text" value={pair.value} onChange={(event) => updatePair(index, 'value', event.target.value)} placeholder="value" disabled={disabled} className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono" />
            <button type="button" onClick={() => removePair(index)} disabled={disabled} className="p-1 text-gray-400 hover:text-red-500 dark:hover:text-red-400 disabled:opacity-50" aria-label="削除">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
          </div>
        ))}
      </div>
      <button type="button" onClick={() => onChange([...pairs, { key: '', value: '' }])} disabled={disabled} className="mt-2 text-sm text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 flex items-center gap-1 disabled:opacity-50">
        <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 6v6m0 0v6m0-6h6m-6 0H6" /></svg>
        + 追加
      </button>
      {helpText && <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">{helpText}</p>}
    </div>
  )
}

export function keyValuePairsToRecord(pairs: KeyValuePair[]): Record<string, string> | undefined {
  const record: Record<string, string> = {}
  pairs.forEach(({ key, value }) => {
    if (key.trim()) record[key.trim()] = value
  })
  return Object.keys(record).length > 0 ? record : undefined
}

export function recordToKeyValuePairs(record?: Record<string, string>): KeyValuePair[] {
  return record && Object.keys(record).length > 0
    ? Object.entries(record).map(([key, value]) => ({ key, value }))
    : [{ key: '', value: '' }]
}
