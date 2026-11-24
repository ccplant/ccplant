import type { Components } from 'react-markdown';
export const markdownComponents: Components = {
  // 見出しのスタイリング - 既存UIのパターンに合わせる
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  h1: ({ node, ...props }) => (
    <h1
      className="text-2xl font-bold text-gray-900 dark:text-white mt-6 mb-4 pb-2 border-b-2 border-gray-200 dark:border-gray-700"
      {...props}
    />
  ),
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  h2: ({ node, ...props }) => (
    <h2
      className="text-xl font-bold text-gray-900 dark:text-white mt-5 mb-3 pb-2 border-b border-gray-200 dark:border-gray-700"
      {...props}
    />
  ),
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  h3: ({ node, ...props }) => (
    <h3 className="text-lg font-semibold text-gray-900 dark:text-white mt-4 mb-2" {...props} />
  ),
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  h4: ({ node, ...props }) => (
    <h4 className="text-base font-semibold text-gray-800 dark:text-gray-100 mt-3 mb-2" {...props} />
  ),
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  h5: ({ node, ...props }) => (
    <h5 className="text-sm font-semibold text-gray-700 dark:text-gray-200 mt-3 mb-2" {...props} />
  ),
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  h6: ({ node, ...props }) => (
    <h6 className="text-sm font-medium text-gray-600 dark:text-gray-300 mt-2 mb-1" {...props} />
  ),
  // リンクに適切なスタイルを追加
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  a: ({ node, ...props }) => (
    <a
      {...props}
      target="_blank"
      rel="noopener noreferrer"
      className="break-words [overflow-wrap:anywhere] text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 underline"
    />
  ),
  // コードブロックのスタイリング
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  code: ({ node, className, children, ...props }) => {
    const isInline = !className;
    return isInline ? (
      <code className="bg-gray-100 dark:bg-gray-800 px-1 py-0.5 rounded text-sm font-mono" {...props}>
        {children}
      </code>
    ) : (
      <code className={className} {...props}>
        {children}
      </code>
    );
  },
  // プリフォーマット済みテキストのスタイリング
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  pre: ({ node, ...props }) => (
    <pre className="bg-gray-100 dark:bg-gray-800 p-3 rounded-md overflow-x-auto" {...props} />
  ),
  // 水平線のスタイリング - より目立つように
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  hr: ({ node, ...props }) => <hr className="my-8 border-t-2 border-gray-200 dark:border-gray-700" {...props} />,
  // テーブルのスタイリング - 既存UIのカードスタイルに合わせる
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  table: ({ node, ...props }) => (
    <div className="overflow-x-auto my-4 -mx-2 px-2">
      <table
        className="min-w-full divide-y divide-gray-200 dark:divide-gray-700 border border-gray-200 dark:border-gray-700 rounded-lg"
        {...props}
      />
    </div>
  ),
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  thead: ({ node, ...props }) => <thead className="bg-gray-50 dark:bg-gray-800" {...props} />,
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  tbody: ({ node, ...props }) => (
    <tbody className="divide-y divide-gray-200 dark:divide-gray-700 bg-white dark:bg-gray-900" {...props} />
  ),
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  tr: ({ node, ...props }) => (
    <tr className="hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors" {...props} />
  ),
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  th: ({ node, ...props }) => (
    <th
      className="px-3 py-2 text-left text-xs sm:text-sm font-semibold text-gray-900 dark:text-white whitespace-nowrap"
      {...props}
    />
  ),
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  td: ({ node, ...props }) => (
    <td
      className="px-3 py-2 text-xs sm:text-sm text-gray-700 dark:text-gray-300 border-t border-gray-100 dark:border-gray-800 whitespace-nowrap"
      {...props}
    />
  ),
  // リストのスタイリング
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  ul: ({ node, ...props }) => (
    <ul className="list-disc list-inside my-3 space-y-1.5 text-gray-700 dark:text-gray-300" {...props} />
  ),
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  ol: ({ node, ...props }) => (
    <ol className="list-decimal list-inside my-3 space-y-1.5 text-gray-700 dark:text-gray-300" {...props} />
  ),
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  li: ({ node, ...props }) => <li className="text-gray-700 dark:text-gray-300 leading-relaxed" {...props} />,
  // 引用のスタイリング - 既存の通知カードスタイルに合わせる
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  blockquote: ({ node, ...props }) => (
    <blockquote
      className="border-l-4 border-gray-200 dark:border-gray-700 pl-4 py-2 my-4 italic text-gray-700 dark:text-gray-300 bg-gray-50 dark:bg-gray-800/50 rounded-r"
      {...props}
    />
  ),
  // 段落のスタイリング
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  p: ({ node, ...props }) => <p className="my-3 text-gray-700 dark:text-gray-300 leading-relaxed" {...props} />,
};
