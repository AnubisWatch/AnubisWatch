import type { ApiError } from '../api/client'

const quotaLabels: Record<string, { noun: string; plural: string }> = {
  max_souls: { noun: 'soul', plural: 'souls' },
  max_journeys: { noun: 'journey', plural: 'journeys' },
  max_alert_channels: { noun: 'alert channel', plural: 'alert channels' },
}

/** True when err is the server refusing a create because of a workspace quota. */
export function isQuotaError(err: unknown): err is ApiError {
  // Duck-typed so it also works where the API client module is mocked.
  return err instanceof Error && (err as Partial<ApiError>).code === 'quota_exceeded'
}

/**
 * A readable sentence for a workspace quota refusal, or null when err is not
 * one. The quota name is kept so an administrator knows which setting to raise.
 */
export function quotaErrorMessage(err: unknown): string | null {
  if (!isQuotaError(err)) return null
  const setting = `tenants.default_quotas.${err.quota ?? 'unknown'}`
  if (err.quota === 'check_interval_min') {
    return `This workspace requires a check interval of at least ${err.limit ?? 'the configured minimum'}. Choose a longer interval, or ask an administrator to lower ${setting}.`
  }
  const label = err.quota ? quotaLabels[err.quota] : undefined
  const plural = label?.plural ?? 'resources'
  const limit = typeof err.limit === 'number' ? err.limit : undefined
  const head = limit !== undefined
    ? `Quota reached: this workspace may hold at most ${limit} ${limit === 1 && label ? label.noun : plural}.`
    : `Quota reached for ${plural} in this workspace.`
  return `${head} Delete one first, or ask an administrator to raise ${setting}.`
}

/** The message to show for a failed request: the quota sentence when it is one. */
export function describeApiError(err: unknown, fallback: string): string {
  return quotaErrorMessage(err) ?? (err instanceof Error ? err.message : fallback)
}
