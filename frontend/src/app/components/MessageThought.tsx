import type { MessageItemProps, MessageThoughtProps } from './messageItemTypes';
export function MessageThought({
  message,
  isThoughtExpanded,
  setIsThoughtExpanded,
}: Pick<MessageItemProps, 'message'> & MessageThoughtProps) {
  return (
    <div className="mb-1">
      <button
        onClick={() => setIsThoughtExpanded(!isThoughtExpanded)}
        className="text-[11px] text-gray-400 dark:text-gray-600 hover:text-gray-500 dark:hover:text-gray-500 transition-colors"
      >
        💭 {isThoughtExpanded ? '思考を隠す' : '思考を表示'}
      </button>
      {isThoughtExpanded && (
        <div className="mt-1 pl-2 border-l border-gray-200 dark:border-gray-700 text-xs text-gray-400 dark:text-gray-500 italic leading-relaxed">
          <div className="whitespace-pre-wrap break-words">{message.thought}</div>
        </div>
      )}
    </div>
  );
}
