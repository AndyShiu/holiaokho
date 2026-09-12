// Package model holds the core domain types shared by every layer.
// Terminology follows docs/holiaokho-architecture-draft.md §2:
// Repository → Package → Asset → Blob.
package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type RepoType string

const (
	Hosted RepoType = "hosted"
	Proxy  RepoType = "proxy"
	Group  RepoType = "group"
)

// WritePolicy mirrors Nexus hosted repository deployment policy.
type WritePolicy string

const (
	WriteAllow     WritePolicy = "allow"
	WriteAllowOnce WritePolicy = "allow_once"
	WriteDeny      WritePolicy = "deny"
)

// Repository is a named store of one format and one type.
// Format-specific settings live in Attributes (opaque to the core).
type Repository struct {
	ID         uuid.UUID       `json:"id"`
	Name       string          `json:"name"`
	Format     string          `json:"format"`
	Type       RepoType        `json:"type"`
	StorageID  uuid.UUID       `json:"storageId"`
	Online     bool            `json:"online"`
	Attributes json.RawMessage `json:"attributes"`
	// RoutingRuleID optionally restricts which paths this repository serves.
	RoutingRuleID *uuid.UUID `json:"routingRuleId"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`

	// Decoded common attribute blocks (populated by RepositoryService).
	Proxy   *ProxyAttrs  `json:"-"`
	Hosted  *HostedAttrs `json:"-"`
	Members []string     `json:"-"` // group members, ordered
}

// ProxyAttrs are the generic proxy settings understood by the core.
type ProxyAttrs struct {
	RemoteURL string `json:"remoteUrl"`
	// ContentMaxAge in minutes; -1 = cache forever (immutable artifacts).
	ContentMaxAge int `json:"contentMaxAge"`
	// MetadataMaxAge in minutes for index / metadata documents.
	MetadataMaxAge int `json:"metadataMaxAge"`
	// NegativeCacheTTL in minutes; 0 disables negative caching.
	NegativeCacheTTL int    `json:"negativeCacheTtl"`
	Username         string `json:"username,omitempty"`
	Password         string `json:"password,omitempty"`
	// Blocked stops all upstream traffic (serve cache only).
	Blocked bool `json:"blocked"`
	// AutoBlock temporarily blocks upstream after repeated failures.
	AutoBlock bool `json:"autoBlock"`
}

type HostedAttrs struct {
	WritePolicy WritePolicy `json:"writePolicy"`
}

// Attributes is the JSON shape of Repository.Attributes.
type Attributes struct {
	Proxy  *ProxyAttrs  `json:"proxy,omitempty"`
	Hosted *HostedAttrs `json:"hosted,omitempty"`
	Group  *GroupAttrs  `json:"group,omitempty"`
	// Format-specific blocks are kept raw, e.g. "maven": {...}, "docker": {...}.
	Extra map[string]json.RawMessage `json:"-"`
}

type GroupAttrs struct {
	Members []string `json:"members"`
}

type Storage struct {
	ID         uuid.UUID       `json:"id"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Config     json.RawMessage `json:"config"`
	QuotaBytes int64           `json:"quotaBytes"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type Blob struct {
	Digest    string    `json:"digest"` // "sha256:<hex>"
	Size      int64     `json:"size"`
	StorageID uuid.UUID `json:"storageId"`
	CreatedAt time.Time `json:"createdAt"`
	RefCount  int64     `json:"refCount"`
}

type Package struct {
	ID               int64           `json:"id"`
	RepoID           uuid.UUID       `json:"repoId"`
	Namespace        string          `json:"namespace"`
	Name             string          `json:"name"`
	Version          string          `json:"version"`
	Attrs            json.RawMessage `json:"attrs"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	LastDownloadedAt *time.Time      `json:"lastDownloadedAt"`
}

type Asset struct {
	ID          int64           `json:"id"`
	RepoID      uuid.UUID       `json:"repoId"`
	PackageID   *int64          `json:"packageId"`
	Path        string          `json:"path"`
	BlobDigest  *string         `json:"blobDigest"`
	Size        int64           `json:"size"`
	ContentType string          `json:"contentType"`
	Attrs       json.RawMessage `json:"attrs"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	// CacheExpiresAt is set for proxy-cached assets; nil = never expires.
	CacheExpiresAt   *time.Time `json:"cacheExpiresAt"`
	LastDownloadedAt *time.Time `json:"lastDownloadedAt"`
	// Negative marks a cached upstream 404.
	Negative bool `json:"negative"`
}

type User struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"displayName"`
	PasswordHash string    `json:"-"`
	Source       string    `json:"source"` // local|ldap|oidc
	Active       bool      `json:"active"`
	Roles        []string  `json:"roles"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Role struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Privileges  []Privilege `json:"privileges"`
	CreatedAt   time.Time   `json:"createdAt"`
}

// Privilege grants Actions on a target. Target forms:
//
//	"repo:<name>"     one repository ("repo:*" = all)
//	"format:<format>" all repositories of a format
//	"app:<area>"      application area (users, roles, repositories, tasks, system, *)
//	"*"               everything
type Privilege struct {
	Target  string   `json:"target"`
	Actions []string `json:"actions"` // read, write, delete, admin, *
}

type Token struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"userId"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

type CleanupPolicy struct {
	ID       uuid.UUID       `json:"id"`
	Name     string          `json:"name"`
	Format   string          `json:"format"` // "" = any
	Criteria CleanupCriteria `json:"criteria"`
}

type CleanupCriteria struct {
	// LastDownloadedDays: remove packages not downloaded in N days (0 = ignore).
	LastDownloadedDays int `json:"lastDownloadedDays"`
	// LastUpdatedDays: remove packages older than N days (0 = ignore).
	LastUpdatedDays int `json:"lastUpdatedDays"`
	// KeepLatest: keep the newest N versions per name (0 = ignore).
	KeepLatest   int    `json:"keepLatest"`
	NameRegex    string `json:"nameRegex"`
	VersionRegex string `json:"versionRegex"`
	// Prerelease: "", "true" (only prereleases) or "false" (only releases).
	Prerelease string `json:"prerelease"`
}

type TaskRun struct {
	ID         int64      `json:"id"`
	TaskName   string     `json:"taskName"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	Status     string     `json:"status"`
	Log        string     `json:"log"`
}
