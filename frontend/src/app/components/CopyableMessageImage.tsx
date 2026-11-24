'use client';
import { useState } from 'react';

interface CopyableMessageImageProps {
  mimeType: string;
  data: string;
  alt: string;
}
export function CopyableMessageImage({ mimeType, data, alt }: CopyableMessageImageProps) {
  const [copyStatus, setCopyStatus] = useState<'idle' | 'copied' | 'error'>('idle');
  const src = `data:${mimeType};base64,${data}`;

  const handleCopy = async () => {
    try {
      if (!navigator.clipboard?.write || typeof ClipboardItem === 'undefined') {
        throw new Error('Image clipboard is not supported');
      }
      const binary = atob(data);
      const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
      const blob = new Blob([bytes], { type: mimeType });
      await navigator.clipboard.write([new ClipboardItem({ [blob.type]: blob })]);
      setCopyStatus('copied');
    } catch (error) {
      console.error('Failed to copy image:', error);
      setCopyStatus('error');
    }
    window.setTimeout(() => setCopyStatus('idle'), 2000);
  };

  return (
    <div className="group relative w-fit max-w-full">
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={src}
        alt={alt}
        className="max-h-96 w-auto max-w-full rounded-lg border border-gray-200 object-contain dark:border-gray-700"
      />
      <button
        type="button"
        onClick={() => void handleCopy()}
        aria-label={`${alt}をコピー`}
        title={
          copyStatus === 'copied' ? 'コピーしました' : copyStatus === 'error' ? 'コピーに失敗しました' : '画像をコピー'
        }
        className={`absolute right-2 top-2 flex h-8 w-8 items-center justify-center rounded-md text-white shadow-md transition-colors ${
          copyStatus === 'copied'
            ? 'bg-green-600'
            : copyStatus === 'error'
              ? 'bg-red-600'
              : 'bg-gray-900/70 hover:bg-gray-900'
        }`}
      >
        {copyStatus === 'copied' ? (
          <span aria-hidden="true">✓</span>
        ) : (
          <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M8 7V5a2 2 0 012-2h8a2 2 0 012 2v10a2 2 0 01-2 2h-2"
            />
            <rect x="4" y="7" width="12" height="14" rx="2" strokeWidth={2} />
          </svg>
        )}
      </button>
    </div>
  );
}
