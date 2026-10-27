/** cron schedule matching the expected interval, for the example line. */
export function cronScheduleFor(interval: string): string {
  const match = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/.exec(interval || '')
  const seconds = match ? (Number(match[1] || 0) * 3600 + Number(match[2] || 0) * 60 + Number(match[3] || 0)) : 0
  if (seconds >= 86400 && seconds % 86400 === 0) return '0 3 * * *'
  if (seconds >= 3600 && seconds % 3600 === 0) return seconds === 3600 ? '0 * * * *' : `0 */${seconds / 3600} * * *`
  if (seconds >= 60 && seconds % 60 === 0) return seconds === 60 ? '* * * * *' : `*/${seconds / 60} * * * *`
  return '* * * * *'
}
