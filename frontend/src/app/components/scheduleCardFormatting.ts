export function formatScheduleDateTime(value?: string): string {
  if (!value) return '-'
  try {
    return new Date(value).toLocaleString('ja-JP', {
      year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
    })
  } catch {
    return value
  }
}
