import type { APIMCPServerConfig } from "@/types/settings";

function serverIcon(type: APIMCPServerConfig["type"]): string {
  return { stdio: "📦", http: "🌐", sse: "📡" }[type] || "⚙️";
}

function serverDescription(config: APIMCPServerConfig): string {
  if (config.type !== "stdio") return config.url || "No URL configured";
  if (!config.command) return "No command configured";
  return `${config.command} ${config.args?.join(" ") || ""}`.trim();
}

function ConfigKeys({ label, keys }: { label: string; keys: string[] }) {
  return keys.length > 0 ? (
    <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">
      {label}: {keys.join(", ")}
    </p>
  ) : null;
}

function ServerCard({ name, config, onEdit, onDelete }: { name: string; config: APIMCPServerConfig; onEdit: () => void; onDelete: () => void }) {
  const envKeys = config.env_keys?.length ? config.env_keys : Object.keys(config.env || {});
  const headerKeys = config.header_keys?.length ? config.header_keys : Object.keys(config.headers || {});
  return (
    <div className="border border-gray-200 dark:border-gray-700 rounded-lg p-4 bg-white dark:bg-gray-800">
      <div className="flex items-start justify-between">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-lg">{serverIcon(config.type)}</span>
            <h4 className="font-medium text-gray-900 dark:text-white truncate">{name}</h4>
            <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300">
              {config.type}
            </span>
          </div>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400 truncate">{serverDescription(config)}</p>
          <ConfigKeys label="Env" keys={envKeys} />
          <ConfigKeys label="Headers" keys={headerKeys} />
        </div>
        <div className="flex gap-2 ml-4">
          <button type="button" onClick={onEdit} className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 text-sm font-medium">
            Edit
          </button>
          <button type="button" onClick={onDelete} className="text-red-600 hover:text-red-800 dark:text-red-400 dark:hover:text-red-300 text-sm font-medium">
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}

export function MCPServerList({
  entries,
  onEdit,
  onDelete,
}: {
  entries: Array<[string, APIMCPServerConfig]>;
  onEdit: (name: string) => void;
  onDelete: (name: string) => void;
}) {
  if (entries.length === 0)
    return (
      <div className="text-center py-8 text-gray-500 dark:text-gray-400">
        <p>No MCP servers configured</p>
        <p className="text-sm mt-1">Click the button below to add your first MCP server</p>
      </div>
    );
  return (
    <div className="space-y-3">
      {entries.map(([name, config]) => (
        <ServerCard key={name} name={name} config={config} onEdit={() => onEdit(name)} onDelete={() => onDelete(name)} />
      ))}
    </div>
  );
}
