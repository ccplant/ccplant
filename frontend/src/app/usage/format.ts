export const formatCompactNumber = (value: number) =>
  new Intl.NumberFormat(undefined, {
    notation: value >= 1000 ? 'compact' : 'standard',
    maximumFractionDigits: 1,
  }).format(value)

export const formatDuration = (seconds: number) => {
  if (seconds < 60) return seconds > 0 ? '<1m' : '0m'
  const minutes = Math.round(seconds / 60)
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  return hours > 0 ? `${hours}h ${rest}m` : `${rest}m`
}
