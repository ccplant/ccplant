'use client';
import React from 'react';
import { MarkdownContent } from './MarkdownContent';
import { hasMarkdownSyntax } from './messageContent';

export function formatTextWithLinks(text: string): React.ReactElement {
  if (!text || typeof text !== 'string') {
    return <>{text || ''}</>;
  }

  const urlRegex = /(https?:\/\/[^\s<>"'()[\]{}]+)/g;
  const parts = text.split(urlRegex);

  return (
    <>
      {parts.map((part, index) => {
        if (/^https?:\/\//.test(part)) {
          const cleanUrl = part.replace(/[.,;!?]+$/, '');
          return (
            <a
              key={index}
              href={cleanUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="break-words [overflow-wrap:anywhere] text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 underline"
            >
              {cleanUrl}
            </a>
          );
        }
        return part;
      })}
    </>
  );
}
export function renderContent(content: string, enableMarkdown: boolean = false): React.ReactElement {
  if (!enableMarkdown || !hasMarkdownSyntax(content)) {
    return (
      <div className="whitespace-pre-wrap break-words overflow-wrap-anywhere max-w-full">
        {formatTextWithLinks(content)}
      </div>
    );
  }
  return <MarkdownContent content={content} />;
}
