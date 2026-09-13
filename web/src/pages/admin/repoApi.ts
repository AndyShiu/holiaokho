import { del, post, put } from '@/api/client'
import type { CleanupPolicy, RepoType, Repository, RoutingRule } from '@/api/types'
import type { RepoFormValues } from '@/components/RepoForm'

export function toPayload(v: RepoFormValues, rules: RoutingRule[], format?: string, type?: RepoType) {
  const attributes: Record<string, unknown> = {}
  for (const [k, val] of Object.entries(v.attributes)) {
    if (!val || typeof val !== 'object') continue
    const cleaned = Object.fromEntries(Object.entries(val).filter(([, x]) => x !== '' && x !== undefined && x !== null))
    attributes[k] = cleaned
  }
  return {
    ...(format ? { name: v.name, format, type } : {}),
    storage: v.storage, online: v.online, attributes,
    routingRule: v.routingRuleId ? rules.find((r) => r.id === v.routingRuleId)?.name ?? '' : '',
  }
}

export async function syncCleanup(repoName: string, wanted: string[], policies: CleanupPolicy[]) {
  const current = policies.filter((p) => p.repositories?.includes(repoName)).map((p) => p.id)
  for (const id of wanted) if (!current.includes(id)) await put(`cleanup-policies/${id}/repositories/${repoName}`)
  for (const id of current) if (!wanted.includes(id)) await del(`cleanup-policies/${id}/repositories/${repoName}`)
}

export function assignedPolicies(repoName: string, policies: CleanupPolicy[]) {
  return policies.filter((p) => p.repositories?.includes(repoName)).map((p) => p.id)
}

export const invalidate = (name: string) => post(`repositories/${name}/invalidate-cache`)
export type { Repository }
