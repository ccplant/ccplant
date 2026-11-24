import type { ConfigPair, MCPServerFormModel } from "./useMCPServerForm";

const inputClass =
  "w-full border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500";

interface PairEditorProps {
  label: string;
  addLabel: string;
  pairs: ConfigPair[];
  onAdd: () => void;
  onRemove: (index: number) => void;
  onChange: (index: number, field: "key" | "value", value: string) => void;
}

export function PairEditor({ label, addLabel, pairs, onAdd, onRemove, onChange }: PairEditorProps) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{label}</label>
      <div className="space-y-2">
        {pairs.map((pair, index) => (
          <div key={index} className="flex gap-2">
            <input
              type="text"
              value={pair.key}
              onChange={(event) => onChange(index, "key", event.target.value)}
              placeholder="Key"
              className={`${inputClass} flex-1 text-sm`}
            />
            <input
              type="password"
              value={pair.value}
              onChange={(event) => onChange(index, "value", event.target.value)}
              placeholder="Value"
              className={`${inputClass} flex-1 text-sm`}
            />
            <button type="button" onClick={() => onRemove(index)} className="px-2 text-red-500 hover:text-red-700" aria-label={`Remove ${label}`}>
              <span aria-hidden>×</span>
            </button>
          </div>
        ))}
        <button type="button" onClick={onAdd} className="text-sm text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300">
          + {addLabel}
        </button>
      </div>
    </div>
  );
}

function ServerIdentityFields({ form }: { form: MCPServerFormModel }) {
  return (
    <>
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          Server Name <span className="text-red-500">*</span>
        </label>
        <input type="text" value={form.name} onChange={(event) => form.setName(event.target.value)} placeholder="e.g., github, slack" className={inputClass} />
      </div>
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
          Type <span className="text-red-500">*</span>
        </label>
        <div className="flex gap-4">
          {(["stdio", "http", "sse"] as const).map((type) => (
            <label key={type} className="flex items-center gap-2 cursor-pointer">
              <input
                type="radio"
                name="type"
                value={type}
                checked={form.config.type === type}
                onChange={() => form.handleTypeChange(type)}
                className="text-blue-600 focus:ring-blue-500"
              />
              <span className="text-gray-700 dark:text-gray-300">{type}</span>
            </label>
          ))}
        </div>
      </div>
    </>
  );
}

function StdioFields({ form }: { form: MCPServerFormModel }) {
  return (
    <>
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          Command <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={form.config.command || ""}
          onChange={(event) => form.setConfig((previous) => ({ ...previous, command: event.target.value }))}
          placeholder="e.g., npx, node, python"
          className={inputClass}
        />
      </div>
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Arguments (one per line)</label>
        <textarea
          value={form.argsText}
          onChange={(event) => form.setArgsText(event.target.value)}
          placeholder={"-y\n@modelcontextprotocol/server-github"}
          rows={3}
          className={`${inputClass} font-mono text-sm`}
        />
      </div>
    </>
  );
}

function RemoteFields({ form }: { form: MCPServerFormModel }) {
  return (
    <>
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
          URL <span className="text-red-500">*</span>
        </label>
        <input
          type="url"
          value={form.config.url || ""}
          onChange={(event) => form.setConfig((previous) => ({ ...previous, url: event.target.value }))}
          placeholder="https://mcp.example.com/server"
          className={inputClass}
        />
      </div>
      <PairEditor
        label="Headers"
        addLabel="Add Header"
        pairs={form.headerPairs}
        onAdd={form.handleAddHeader}
        onRemove={form.handleRemoveHeader}
        onChange={form.handleHeaderChange}
      />
    </>
  );
}

export function MCPServerConnectionFields({ form }: { form: MCPServerFormModel }) {
  return (
    <>
      <ServerIdentityFields form={form} />
      {form.config.type === "stdio" ? <StdioFields form={form} /> : <RemoteFields form={form} />}
    </>
  );
}

export function MCPServerEnvironmentFields({ form }: { form: MCPServerFormModel }) {
  return (
    <PairEditor
      label="Environment Variables"
      addLabel="Add Variable"
      pairs={form.envPairs}
      onAdd={form.handleAddEnv}
      onRemove={form.handleRemoveEnv}
      onChange={form.handleEnvChange}
    />
  );
}
