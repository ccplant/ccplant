export interface ToolUseContent {
  type: 'tool_use';
  name: string;
  id: string;
  input: Record<string, unknown>;
  /** ACP tool title (e.g. full command text or file path summary). Shown in brief info area. */
  title?: string;
}
export interface ToolResultContent {
  result?: string;
  error?: string;
}
export function parseToolUseContent(content: string): ToolUseContent | null {
  try {
    const parsed = JSON.parse(content);
    if (parsed.type === 'tool_use' && parsed.name && parsed.id) {
      return parsed as ToolUseContent;
    }
  } catch {
    return null;
  }
  return null;
}

export function parseToolResultContent(content: string): ToolResultContent | null {
  try {
    return JSON.parse(content) as ToolResultContent;
  } catch {
    return null;
  }
}

export function hasObjectEntries(value: Record<string, unknown> | undefined): boolean {
  return !!value && Object.keys(value).length > 0;
}

export function visibleToolInput(input: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([key]) => key !== '_toolName'));
}

export function isGenericToolTitle(title: string | undefined, toolName: string): boolean {
  if (!title) return true;
  const normalizedTitle = title.trim().toLowerCase();
  const normalizedName = toolName.trim().toLowerCase();
  if (!normalizedTitle) return true;
  return (
    normalizedTitle === normalizedName ||
    normalizedTitle === `${normalizedName} file` ||
    normalizedTitle === `${normalizedName} tool`
  );
}

export function truncateBrief(text: string, maxLength: number): string {
  return text.length > maxLength ? text.substring(0, maxLength) + '...' : text;
}

export function firstMeaningfulLine(text: string): string | null {
  const line = text
    .split('\n')
    .map((part) => part.trim())
    .find((part) => part.length > 0);
  return line || null;
}

export function getToolResultBrief(
  result: ToolResultContent | null,
  fallbackContent: string | undefined,
  maxLength: number
): string | null {
  const content = result?.error || result?.result || fallbackContent;
  if (!content) return null;

  if (typeof content === 'string') {
    const line = firstMeaningfulLine(content);
    return line ? truncateBrief(line, maxLength) : null;
  }

  return null;
}

export function hasMarkdownSyntax(text: string): boolean {
  if (!text || typeof text !== 'string') return false;

  // よくある Markdown の記法をチェック
  const markdownPatterns = [
    /^#{1,6}\s/m, // 見出し (# ## ### など)
    /\*\*[^*]+\*\*/, // 太字 (**text**)
    /\*[^*]+\*/, // イタリック (*text*)
    /`[^`]+`/, // インラインコード (`code`)
    /```[\s\S]*?```/, // コードブロック (```code```)
    /^\s*[-*+]\s/m, // リスト (- * +)
    /^\s*\d+\.\s/m, // 番号付きリスト (1. 2. など)
    /\[[^\]]+\]\([^)]+\)/, // リンク [text](url)
    /!\[[^\]]*\]\([^)]+\)/, // 画像 ![alt](url)
    /^\s*>\s/m, // 引用 (>)
    /^\s*---+\s*$/m, // 水平線 (---)
    /\|.*\|/, // テーブル (| col |)
  ];

  return markdownPatterns.some((pattern) => pattern.test(text));
}
export function getToolBriefInfo(
  toolUse: ToolUseContent,
  result: ToolResultContent | null,
  fallbackContent: string | undefined
): string | null {
  const input = toolUse.input;
  const maxLength = 40;

  // description があれば優先
  if (input.description && typeof input.description === 'string') {
    return truncateBrief(input.description, maxLength);
  }

  // ファイル系のツール
  if (input.file_path && typeof input.file_path === 'string') {
    const path = input.file_path;
    return path.length > maxLength ? '...' + path.substring(path.length - maxLength) : path;
  }

  // パターン検索系
  if (input.pattern && typeof input.pattern === 'string') {
    return truncateBrief(input.pattern, maxLength);
  }

  // コマンド系
  if (input.command && typeof input.command === 'string') {
    return truncateBrief(input.command, maxLength);
  }

  // ACP title は補助情報として使うが、"Read File" のような汎用 title は抑制する
  if (toolUse.title && typeof toolUse.title === 'string' && !isGenericToolTitle(toolUse.title, toolUse.name)) {
    return truncateBrief(toolUse.title, maxLength);
  }

  return getToolResultBrief(result, fallbackContent, maxLength);
}
