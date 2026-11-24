'use client';

import { useState } from 'react';
import { AgentAPIProxyClient } from '../../lib/agentapi-proxy-client';
import ShareSessionModal from './ShareSessionModal';

interface ShareSessionButtonProps { sessionId: string; agentAPI: AgentAPIProxyClient; }

export default function ShareSessionButton({ sessionId, agentAPI }: ShareSessionButtonProps) {
  const [isOpen, setIsOpen] = useState(false);
  return <>
    <button onClick={() => setIsOpen(true)} className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors" title="セッションを共有">
      <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M8.684 13.342C8.886 12.938 9 12.482 9 12c0-.482-.114-.938-.316-1.342m0 2.684a3 3 0 110-2.684m0 2.684l6.632 3.316m-6.632-6l6.632-3.316m0 0a3 3 0 105.367-2.684 3 3 0 00-5.367 2.684zm0 9.316a3 3 0 105.368 2.684 3 3 0 00-5.368-2.684z" /></svg>
    </button>
    {isOpen && <ShareSessionModal sessionId={sessionId} agentAPI={agentAPI} onClose={() => setIsOpen(false)} />}
  </>;
}
