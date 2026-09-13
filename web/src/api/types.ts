export interface Privilege { target: string; actions: string[] }
export interface Session { username: string; roles: string[]; anonymous: boolean; via: string; privileges: Privilege[] }
export interface PasswordPolicy {
  minLength: number; maxLength: number; requireUpper: boolean; requireLower: boolean; requireDigit: boolean
  requireSymbol: boolean; disallowUsername: boolean; disallowCommon: boolean
}
export interface AuthMethods { local: boolean; ldap: boolean; oidc: boolean; oidcLoginUrl: string; anonymous: boolean; passwordPolicy: PasswordPolicy }

export type RepoType = 'hosted' | 'proxy' | 'group'
export interface RepoAttributes {
  proxy?: { remoteUrl?: string; contentMaxAge?: number; metadataMaxAge?: number; negativeCacheTtl?: number; username?: string; password?: string; blocked?: boolean; autoBlock?: boolean }
  hosted?: { writePolicy?: 'allow' | 'allow_once' | 'deny' }
  group?: { members?: string[] }
  maven?: { layoutPolicy?: string; versionPolicy?: string }
  docker?: { httpPort?: number; httpsPort?: number; tlsCert?: string; tlsKey?: string; subdomain?: string; forceBasicAuth?: boolean; indexType?: string; pathEnabled?: boolean }
  apt?: { distribution?: string; component?: string; signingKey?: string; passphrase?: string; flat?: boolean }
  yum?: { repodataDepth?: number; signingKey?: string; passphrase?: string }
  alpine?: { signingKey?: string; keyName?: string }
  cargo?: { downloadUrl?: string }
  [k: string]: any
}
export interface Repository {
  id: string; name: string; format: string; type: RepoType; storageId: string; online: boolean
  attributes: RepoAttributes; routingRuleId?: string | null; createdAt: string; updatedAt: string
  url?: string; stats?: { packages: number; assets: number; size: number }
  cleanupPolicies?: string[]
}
export interface Storage { id: string; name: string; type: 'fs' | 's3'; config: Record<string, any>; quotaBytes: number; createdAt: string }
export interface Package { id: string; repoId: string; namespace: string; name: string; version: string; attrs: Record<string, any>; createdAt: string; updatedAt: string; lastDownloadedAt?: string | null; repository?: string; format?: string }
export interface Asset { id: string; repoId: string; packageId?: string | null; path: string; blobDigest: string; size: number; contentType: string; attrs: Record<string, any>; createdAt: string; updatedAt: string; cacheExpiresAt?: string | null; lastDownloadedAt?: string | null; negative: boolean }
export interface BrowseResult { path: string; directories: string[]; files: Asset[] }
export interface User { id: string; username: string; email: string; displayName: string; source: string; active: boolean; roles: string[]; createdAt: string }
export interface Role { id: string; name: string; description: string; privileges: Privilege[]; createdAt: string }
export interface Token { id: string; userId: string; name: string; prefix: string; createdAt: string; expiresAt?: string | null; lastUsedAt?: string | null }
export interface Task { name: string; description: string; interval: string; cron?: string; enabled: boolean; lastRun?: string | null; nextRun?: string | null; running: boolean; lastStatus?: string }
export interface TaskRun { id: string; taskName: string; startedAt: string; finishedAt?: string | null; status: string; log: string }
export interface CleanupPolicy { id: string; name: string; format: string; criteria: { lastDownloadedDays?: number; lastUpdatedDays?: number; keepLatest?: number; nameRegex?: string; versionRegex?: string; prerelease?: string }; repositories?: string[]; createdAt?: string }
export interface RoutingRule { id: string; name: string; description: string; mode: 'allow' | 'block'; matchers: string[]; createdAt: string }
export interface ContentSelector { id: string; name: string; description: string; expression: string; createdAt: string }
export interface Webhook { id: string; name: string; url: string; secret?: string; events: string[]; repository: string; enabled: boolean; createdAt: string }
export interface EmailSettings { enabled: boolean; host: string; port: number; username: string; password?: string; from: string; startTls: boolean; ssl: boolean; recipients: string[] }
export interface BackupSettings { enabled: boolean; dir: string; withBlobs: boolean; keep: number; cron: string }
export interface AuditEntry { id: string; at: string; actor: string; action: string; targetType: string; targetId: string; detail?: any }
export interface HealthCheck { healthy: boolean; code?: string; message?: string; type?: string; usedBytes?: number; quotaBytes?: number }
export interface Health { healthy: boolean; checks: Record<string, HealthCheck> }
export interface Status { name: string; version: string; uptime: string; formats: string[]; baseUrl?: string }
export interface LDAPConfig { enabled: boolean; url: string; startTls: boolean; insecureSkipVerify: boolean; bindDn: string; bindPassword?: string; userBaseDn: string; userFilter: string; userSubtree: boolean; emailAttr: string; displayNameAttr: string; groupBaseDn: string; groupFilter: string; groupNameAttr: string; memberOfAttr: string; roleMapping: Record<string, string>; defaultRoles: string[]; timeoutSeconds: number }
export interface OIDCConfig { enabled: boolean; issuer: string; clientId: string; clientSecret?: string; scopes: string[]; usernameClaim: string; groupsClaim: string; roleMapping: Record<string, string>; defaultRoles: string[]; redirectUrl: string; insecureSkipIssuerVerify: boolean }
export interface RutConfig { enabled: boolean; header: string; trustedProxies: string[]; autoCreate: boolean; defaultRoles: string[] }
export interface SearchHit extends Package { repository: string; format: string }
export interface AuthSettings { realms: string[]; defaultRoles: string[]; password: PasswordPolicy; anonymous?: boolean | null; ldap: LDAPConfig; oidc: OIDCConfig; rut: RutConfig }
