"use client";

import type { APIMCPServerConfig } from "@/types/settings";
import type { EditingServer } from "./useMCPServerSettings";
import { MCPServerConnectionFields, MCPServerEnvironmentFields } from "./MCPServerFormFields";
import { useMCPServerForm } from "./useMCPServerForm";

interface MCPServerModalProps {
  server: EditingServer;
  existingNames: string[];
  onSave: (name: string, config: APIMCPServerConfig) => void;
  onClose: () => void;
}

function ModalHeader({ server, onClose }: Pick<MCPServerModalProps, "server" | "onClose">) {
  return (
    <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
      <h3 className="text-lg font-semibold text-gray-900 dark:text-white">{server.isNew ? "Add MCP Server" : "Edit MCP Server"}</h3>
      <button type="button" onClick={onClose} className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close">
        <span aria-hidden>×</span>
      </button>
    </div>
  );
}

function ModalFooter({ onClose, onSave }: { onClose: () => void; onSave: () => void }) {
  return (
    <div className="flex justify-end gap-3 p-4 border-t border-gray-200 dark:border-gray-700">
      <button
        type="button"
        onClick={onClose}
        className="px-4 py-2 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
      >
        Cancel
      </button>
      <button type="button" onClick={onSave} className="px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 transition-colors">
        Save
      </button>
    </div>
  );
}

export function MCPServerModal({ server, existingNames, onSave, onClose }: MCPServerModalProps) {
  const form = useMCPServerForm(server, existingNames, onSave);
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black bg-opacity-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg mx-4 max-h-[90vh] overflow-y-auto">
        <ModalHeader server={server} onClose={onClose} />
        <div className="p-4 space-y-4">
          {form.error && (
            <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
              <p className="text-red-600 dark:text-red-400 text-sm">{form.error}</p>
            </div>
          )}
          <MCPServerConnectionFields form={form} />
          <MCPServerEnvironmentFields form={form} />
        </div>
        <ModalFooter onClose={onClose} onSave={form.handleSave} />
      </div>
    </div>
  );
}
