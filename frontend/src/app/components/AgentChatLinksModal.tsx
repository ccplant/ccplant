'use client';

import { getGitHubLinkParts } from './agentChatPresentation';

import type { useAgentChat } from './useAgentChat';

export function AgentChatLinksModal(state: ReturnType<typeof useAgentChat>) {
  const { showPRLinks, setShowPRLinks, relatedLinks } = state;
  return (
    <>
      {showPRLinks && (
        <div
          className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4"
          onClick={(e) => {
            if (e.target === e.currentTarget) {
              setShowPRLinks(false);
            }
          }}
        >
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-2xl w-full max-h-[80vh] overflow-hidden">
            <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
                  関連リンク ({relatedLinks.length}件)
                </h2>
                <button
                  onClick={() => setShowPRLinks(false)}
                  className="text-gray-400 hover:text-gray-500 dark:hover:text-gray-300"
                >
                  <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>
            </div>

            <div className="overflow-y-auto max-h-[calc(80vh-8rem)] px-6 py-4">
              {relatedLinks.length > 0 ? (
                <div className="space-y-3">
                  {relatedLinks.map((link, index) => {
                    const { domain, number, repoName, isGitHubCom } = getGitHubLinkParts(link.url, link.type);
                    const label = link.type === 'pr' ? 'Pull Request' : 'Issue';
                    const shortLabel = link.type === 'pr' ? 'PR' : 'Issue';

                    return (
                      <div
                        key={index}
                        className="flex min-w-0 items-center justify-between gap-3 p-3 bg-gray-50 dark:bg-gray-700 rounded-md"
                      >
                        <div className="flex-1 min-w-0">
                          <div className="break-words [overflow-wrap:anywhere] font-medium text-sm text-gray-900 dark:text-white">
                            {repoName || link.url}
                          </div>
                          <div className="break-words [overflow-wrap:anywhere] text-xs text-gray-500 dark:text-gray-400">
                            {!isGitHubCom && <span>{domain} • </span>}
                            {label}
                            {number ? ` #${number}` : ''}
                          </div>
                        </div>
                        <div className="flex flex-shrink-0 items-center space-x-2">
                          <button
                            onClick={() => navigator.clipboard.writeText(link.url)}
                            className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 rounded"
                            title="URLをコピー"
                          >
                            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                strokeWidth={2}
                                d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
                              />
                            </svg>
                          </button>
                          <a
                            href={link.url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center px-3 py-1 bg-gray-900 hover:bg-gray-800 dark:bg-gray-100 dark:hover:bg-gray-200 text-white dark:text-gray-900 text-xs rounded-md transition-colors"
                          >
                            {shortLabel}
                            <svg className="w-3 h-3 ml-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path
                                strokeLinecap="round"
                                strokeLinejoin="round"
                                strokeWidth={2}
                                d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"
                              />
                            </svg>
                          </a>
                        </div>
                      </div>
                    );
                  })}
                </div>
              ) : (
                <div className="text-center py-8 text-gray-500 dark:text-gray-400">
                  <svg
                    className="w-12 h-12 mx-auto mb-3 text-gray-300 dark:text-gray-600"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={1}
                      d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4"
                    />
                  </svg>
                  <p className="text-sm">関連リンクが見つかりませんでした</p>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
