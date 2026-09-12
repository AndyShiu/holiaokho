// Package nexusimport migrates a Sonatype Nexus Repository 3 instance
// (PostgreSQL-backed, 3.7x+ schema) into Holiaokho: repositories, users,
// roles, cleanup policies and content. It reads Nexus read-only and is
// idempotent, so it can be run repeatedly before the final cut-over.
package nexusimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/storage"
)

type Options struct {
	NexusDB   string // postgres://user:pass@host/nexus
	BlobsDir  string // path to <nexus-data>/blobs
	Link      bool   // hardlink instead of copy
	DryRun    bool
	Formats   []string // nexus format names to import content for
	NoContent bool     // only configuration, users and policies
	NoUsers   bool
}

type Importer struct {
	Content *content.Service
	Auth    *auth.Service
	Log     *slog.Logger
	Opt     Options

	nx    *pgxpool.Pool
	stats map[string]int
	// nexus repository id -> our repository
	repos map[string]*model.Repository
}

func Run(ctx context.Context, c *content.Service, a *auth.Service, log *slog.Logger, opt Options) error {
	if len(opt.Formats) == 0 {
		opt.Formats = []string{"maven2", "npm", "docker"}
	}
	nx, err := pgxpool.New(ctx, opt.NexusDB)
	if err != nil {
		return fmt.Errorf("connect nexus db: %w", err)
	}
	defer nx.Close()
	if err := nx.Ping(ctx); err != nil {
		return fmt.Errorf("ping nexus db: %w", err)
	}
	im := &Importer{Content: c, Auth: a, Log: log, Opt: opt, nx: nx, stats: map[string]int{}, repos: map[string]*model.Repository{}}
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"repositories", im.importRepositories},
		{"cleanup policies", im.importCleanupPolicies},
		{"roles and users", im.importUsers},
		{"content", im.importContent},
	}
	for _, s := range steps {
		if s.name == "content" && opt.NoContent {
			continue
		}
		if s.name == "roles and users" && opt.NoUsers {
			continue
		}
		log.Info("import step", "step", s.name)
		if err := s.fn(ctx); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}
	log.Info("import finished", "stats", im.stats)
	return nil
}

func (im *Importer) count(k string) { im.stats[k]++ }

// ----------------------------------------------------------- repositories

var recipeRe = regexp.MustCompile(`^([a-z0-9]+)-(hosted|proxy|group)$`)

func formatName(nexus string) string {
	switch nexus {
	case "maven2":
		return "maven"
	}
	return nexus
}

func (im *Importer) importRepositories(ctx context.Context) error {
	rows, err := im.nx.Query(ctx, `SELECT id, name, recipe_name, online, attributes FROM repository ORDER BY CASE WHEN recipe_name LIKE '%-group' THEN 1 ELSE 0 END, name`)
	if err != nil {
		return err
	}
	type nrepo struct {
		id, name, recipe string
		online           bool
		attrs            map[string]json.RawMessage
	}
	var list []nrepo
	for rows.Next() {
		var r nrepo
		var raw []byte
		if err := rows.Scan(&r.id, &r.name, &r.recipe, &r.online, &raw); err != nil {
			rows.Close()
			return err
		}
		json.Unmarshal(raw, &r.attrs)
		list = append(list, r)
	}
	rows.Close()
	for _, r := range list {
		m := recipeRe.FindStringSubmatch(r.recipe)
		if m == nil {
			im.Log.Warn("skip repository with unknown recipe", "name", r.name, "recipe", r.recipe)
			continue
		}
		rp := &model.Repository{Name: r.name, Format: formatName(m[1]), Type: model.RepoType(m[2]), Online: r.online}
		attrs := map[string]any{}
		get := func(block string) map[string]any {
			var out map[string]any
			if raw, ok := r.attrs[block]; ok {
				json.Unmarshal(raw, &out)
			}
			return out
		}
		switch rp.Type {
		case model.Proxy:
			p := get("proxy")
			hc := get("httpclient")
			neg := get("negativeCache")
			pa := model.ProxyAttrs{RemoteURL: str(p["remoteUrl"]), ContentMaxAge: num(p["contentMaxAge"], 1440), MetadataMaxAge: num(p["metadataMaxAge"], 1440), NegativeCacheTTL: 1}
			if neg != nil {
				if en, _ := neg["enabled"].(bool); !en {
					pa.NegativeCacheTTL = 0
				} else {
					pa.NegativeCacheTTL = num(neg["timeToLive"], 60) / 1
				}
			}
			if hc != nil {
				pa.Blocked, _ = hc["blocked"].(bool)
				pa.AutoBlock, _ = hc["autoBlock"].(bool)
				if a, ok := hc["authentication"].(map[string]any); ok {
					pa.Username = str(a["username"])
					// Passwords are encrypted with the Nexus secret key and cannot be read here.
				}
			}
			attrs["proxy"] = pa
		case model.Hosted:
			st := get("storage")
			wp := model.WriteAllow
			switch strings.ToUpper(str(st["writePolicy"])) {
			case "ALLOW_ONCE":
				wp = model.WriteAllowOnce
			case "DENY":
				wp = model.WriteDeny
			}
			attrs["hosted"] = model.HostedAttrs{WritePolicy: wp}
		case model.Group:
			g := get("group")
			var members []string
			if mn, ok := g["memberNames"].([]any); ok {
				for _, x := range mn {
					members = append(members, str(x))
				}
			}
			attrs["group"] = model.GroupAttrs{Members: members}
		}
		switch rp.Format {
		case "maven":
			mv := get("maven")
			attrs["maven"] = map[string]any{"layoutPolicy": strings.ToUpper(str(mv["layoutPolicy"])), "versionPolicy": strings.ToUpper(str(mv["versionPolicy"]))}
		case "docker":
			d := get("docker")
			dp := get("dockerProxy")
			da := map[string]any{"httpPort": num(d["httpPort"], 0), "forceBasicAuth": boolean(d["forceBasicAuth"]), "pathEnabled": true}
			if it := str(dp["indexType"]); it != "" {
				da["indexType"] = it
			}
			attrs["docker"] = da
		}
		rp.Attributes, _ = json.Marshal(attrs)
		existing, err := im.Content.Repo(ctx, rp.Name)
		if err == nil {
			im.Log.Info("repository exists, updating attributes", "name", rp.Name)
			existing.Attributes = rp.Attributes
			existing.Online = rp.Online
			if !im.Opt.DryRun {
				if err := im.Content.UpdateRepo(ctx, existing); err != nil {
					return fmt.Errorf("update %s: %w", rp.Name, err)
				}
			}
			im.repos[r.id] = existing
			im.count("repositories.updated")
			continue
		}
		if !errors.Is(err, content.ErrNotFound) {
			return err
		}
		im.Log.Info("creating repository", "name", rp.Name, "format", rp.Format, "type", rp.Type)
		if !im.Opt.DryRun {
			if err := im.Content.CreateRepo(ctx, rp); err != nil {
				return fmt.Errorf("create %s: %w", rp.Name, err)
			}
			rp, _ = im.Content.Repo(ctx, rp.Name)
		}
		im.repos[r.id] = rp
		im.count("repositories.created")
	}
	return nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
func num(v any, def int) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		if n, err := strconv.Atoi(x); err == nil {
			return n
		}
	}
	return def
}
func boolean(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true"
	}
	return false
}

// -------------------------------------------------------- cleanup policies

func (im *Importer) importCleanupPolicies(ctx context.Context) error {
	rows, err := im.nx.Query(ctx, `SELECT name, format, criteria FROM cleanup_policy`)
	if err != nil {
		return err
	}
	defer rows.Close()
	byName := map[string]uuid.UUID{}
	for rows.Next() {
		var name, format string
		var raw []byte
		if err := rows.Scan(&name, &format, &raw); err != nil {
			return err
		}
		var crit map[string]any
		json.Unmarshal(raw, &crit)
		p := model.CleanupPolicy{Name: name, Format: formatName(format)}
		if format == "ALL" || format == "*" {
			p.Format = ""
		}
		if v := num(crit["lastDownloaded"], 0); v > 0 {
			p.Criteria.LastDownloadedDays = v / 86400
		}
		if v := num(crit["lastBlobUpdated"], 0); v > 0 {
			p.Criteria.LastUpdatedDays = v / 86400
		}
		if v := str(crit["regex"]); v != "" {
			p.Criteria.NameRegex = v
		}
		if v := num(crit["retain"], 0); v > 0 {
			p.Criteria.KeepLatest = v
		}
		if v, ok := crit["isPrerelease"]; ok {
			if boolean(v) {
				p.Criteria.Prerelease = "true"
			} else {
				p.Criteria.Prerelease = "false"
			}
		}
		cj, _ := json.Marshal(p.Criteria)
		var id uuid.UUID
		if im.Opt.DryRun {
			continue
		}
		err := im.Content.DB.Pool.QueryRow(ctx, `INSERT INTO cleanup_policies(id,name,format,criteria) VALUES ($1,$2,$3,$4)
			ON CONFLICT (name) DO UPDATE SET format=EXCLUDED.format, criteria=EXCLUDED.criteria RETURNING id`, uuid.New(), p.Name, p.Format, cj).Scan(&id)
		if err != nil {
			return err
		}
		byName[name] = id
		im.count("cleanup_policies")
	}
	// Assignments live in repository.attributes.cleanup.policyName[].
	rrows, err := im.nx.Query(ctx, `SELECT id, attributes->'cleanup'->'policyName' FROM repository WHERE attributes ? 'cleanup'`)
	if err != nil {
		return err
	}
	defer rrows.Close()
	for rrows.Next() {
		var rid string
		var raw []byte
		if err := rrows.Scan(&rid, &raw); err != nil {
			return err
		}
		var names []string
		json.Unmarshal(raw, &names)
		rp := im.repos[rid]
		if rp == nil {
			continue
		}
		for _, n := range names {
			if pid, ok := byName[n]; ok && !im.Opt.DryRun {
				im.Content.DB.Pool.Exec(ctx, `INSERT INTO repo_cleanup_policies(repo_id,policy_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, rp.ID, pid)
			}
		}
	}
	return nil
}

// ------------------------------------------------------------------ users

var privRe = regexp.MustCompile(`^nx-repository-(view|admin)-([a-z0-9*]+)-(.+)-(browse|read|edit|add|delete|\*)$`)

func mapPrivilege(name string) *model.Privilege {
	switch {
	case name == "nx-all":
		return &model.Privilege{Target: "*", Actions: []string{"*"}}
	case name == "nx-search-read":
		return &model.Privilege{Target: "app:search", Actions: []string{auth.Read}}
	case name == "nx-healthcheck-read", name == "nx-status-read":
		return &model.Privilege{Target: "app:status", Actions: []string{auth.Read}}
	case strings.HasPrefix(name, "nx-repository-"):
		m := privRe.FindStringSubmatch(name)
		if m == nil {
			return nil
		}
		format, repo, action := m[2], m[3], m[4]
		target := "repo:" + repo
		if repo == "*" {
			if format == "*" {
				target = "repo:*"
			} else {
				target = "format:" + formatName(format)
			}
		}
		var actions []string
		switch action {
		case "browse", "read":
			actions = []string{auth.Read}
		case "edit", "add":
			actions = []string{auth.Write}
		case "delete":
			actions = []string{auth.Delete}
		case "*":
			actions = []string{"*"}
		}
		if m[1] == "admin" {
			actions = []string{auth.Admin}
		}
		return &model.Privilege{Target: target, Actions: actions}
	}
	// Application privileges: nx-<area>-<action>
	parts := strings.Split(strings.TrimPrefix(name, "nx-"), "-")
	if len(parts) >= 2 {
		area := strings.Join(parts[:len(parts)-1], "-")
		act := parts[len(parts)-1]
		areaMap := map[string]string{"users": "app:users", "roles": "app:roles", "privileges": "app:roles", "repository-admin": "app:repositories",
			"blobstores": "app:storages", "tasks": "app:tasks", "settings": "app:system", "audit": "app:system"}
		t, ok := areaMap[area]
		if !ok {
			return nil
		}
		var actions []string
		switch act {
		case "read":
			actions = []string{auth.Read}
		case "create", "update":
			actions = []string{auth.Write}
		case "delete":
			actions = []string{auth.Delete}
		case "all", "*":
			actions = []string{"*"}
		default:
			return nil
		}
		return &model.Privilege{Target: t, Actions: actions}
	}
	return nil
}

func mapRoleID(id string) string {
	switch id {
	case "nx-admin":
		return auth.RoleAdmin
	case "nx-anonymous":
		return auth.RoleAnonymous
	}
	return id
}

func (im *Importer) importUsers(ctx context.Context) error {
	// Custom roles.
	rows, err := im.nx.Query(ctx, `SELECT id, name, description, privileges, roles FROM role`)
	if err != nil {
		return err
	}
	type nrole struct {
		id, name, desc string
		privs, roles   []string
	}
	var roles []nrole
	for rows.Next() {
		var r nrole
		var p, rr []byte
		if err := rows.Scan(&r.id, &r.name, &r.desc, &p, &rr); err != nil {
			rows.Close()
			return err
		}
		json.Unmarshal(p, &r.privs)
		json.Unmarshal(rr, &r.roles)
		roles = append(roles, r)
	}
	rows.Close()
	for _, r := range roles {
		if r.id == "nx-admin" || r.id == "nx-anonymous" {
			continue
		}
		role := &model.Role{ID: mapRoleID(r.id), Name: r.name, Description: r.desc}
		for _, p := range r.privs {
			if mp := mapPrivilege(p); mp != nil {
				role.Privileges = append(role.Privileges, *mp)
			} else {
				im.Log.Warn("privilege not mapped", "role", r.id, "privilege", p)
			}
		}
		if len(r.roles) > 0 {
			im.Log.Warn("nested roles are flattened manually; check role", "role", r.id, "contains", r.roles)
		}
		if im.Opt.DryRun {
			continue
		}
		err := im.Auth.SaveRole(ctx, role, true)
		if errors.Is(err, auth.ErrConflict) {
			err = im.Auth.SaveRole(ctx, role, false)
		}
		if err != nil {
			return fmt.Errorf("role %s: %w", r.id, err)
		}
		im.count("roles")
	}
	// Users (local realm only; LDAP users are resolved at login time).
	urows, err := im.nx.Query(ctx, `SELECT u.id, u.first_name, u.last_name, u.email, u.status, u.password, coalesce(m.roles, '[]'::jsonb)
		FROM security_user u LEFT JOIN user_role_mapping m ON m.user_id = u.id AND m.source = 'default'`)
	if err != nil {
		return err
	}
	defer urows.Close()
	for urows.Next() {
		var id, first, last, email, status, pw string
		var raw []byte
		if err := urows.Scan(&id, &first, &last, &email, &status, &pw, &raw); err != nil {
			return err
		}
		if id == "anonymous" {
			continue
		}
		var nroles []string
		json.Unmarshal(raw, &nroles)
		var ours []string
		for _, r := range nroles {
			ours = append(ours, mapRoleID(r))
		}
		u := &model.User{Username: id, Email: email, DisplayName: strings.TrimSpace(first + " " + last), PasswordHash: pw, Source: "local", Active: status == "active", Roles: ours}
		if im.Opt.DryRun {
			continue
		}
		err := im.Auth.CreateUser(ctx, u)
		if errors.Is(err, auth.ErrConflict) {
			existing, _ := im.Auth.User(ctx, id)
			if existing != nil {
				existing.Email, existing.DisplayName, existing.Active, existing.Roles = u.Email, u.DisplayName, u.Active, u.Roles
				if err := im.Auth.UpdateUser(ctx, existing); err != nil {
					return err
				}
				if strings.HasPrefix(existing.PasswordHash, "$shiro1$") || existing.PasswordHash == "" {
					im.Auth.SetPasswordHash(ctx, id, pw)
				}
			}
			im.count("users.updated")
			continue
		}
		if err != nil {
			return fmt.Errorf("user %s: %w", id, err)
		}
		im.count("users.created")
	}
	return nil
}

// ---------------------------------------------------------------- content

type nasset struct {
	repoID    string
	path      string
	kind      string
	blobRef   string
	size      int64
	checksums map[string]string
	ctype     string
	created   time.Time
	lastDl    *time.Time
	ns, name  string
	version   string
	baseVer   *string
	hasComp   bool
}

func (im *Importer) importContent(ctx context.Context) error {
	for _, f := range im.Opt.Formats {
		if err := im.importFormat(ctx, f); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	return nil
}

func (im *Importer) importFormat(ctx context.Context, f string) error {
	q := fmt.Sprintf(`SELECT cr.config_repository_id::text, a.path, a.kind, b.blob_ref, b.blob_size, b.checksums, coalesce(b.content_type,''), a.created, a.last_downloaded,
		coalesce(c.namespace,''), coalesce(c.name,''), coalesce(c.version,''), %s, c.component_id IS NOT NULL
		FROM %s_asset a
		JOIN %s_content_repository cr ON cr.repository_id = a.repository_id
		LEFT JOIN %s_asset_blob b ON b.asset_blob_id = a.asset_blob_id
		LEFT JOIN %s_component c ON c.component_id = a.component_id
		ORDER BY a.asset_id`, baseVersionExpr(f), f, f, f, f)
	rows, err := im.nx.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var a nasset
		var blobRef, ctype *string
		var size *int64
		var checks []byte
		if err := rows.Scan(&a.repoID, &a.path, &a.kind, &blobRef, &size, &checks, &ctype, &a.created, &a.lastDl, &a.ns, &a.name, &a.version, &a.baseVer, &a.hasComp); err != nil {
			return err
		}
		if blobRef == nil {
			continue
		}
		a.blobRef = *blobRef
		if size != nil {
			a.size = *size
		}
		if ctype != nil {
			a.ctype = *ctype
		}
		json.Unmarshal(checks, &a.checksums)
		rp := im.repos[a.repoID]
		if rp == nil {
			continue
		}
		if err := im.importAsset(ctx, f, rp, &a); err != nil {
			im.Log.Warn("asset skipped", "repo", rp.Name, "path", a.path, "err", err)
			im.count("assets.skipped")
			continue
		}
		n++
		if n%500 == 0 {
			im.Log.Info("progress", "format", f, "assets", n)
		}
	}
	im.Log.Info("format done", "format", f, "assets", n)
	if f == "docker" && !im.Opt.DryRun {
		return im.linkDockerBlobs(ctx)
	}
	return nil
}

func baseVersionExpr(f string) string {
	if f == "maven2" {
		return "c.base_version"
	}
	return "NULL::text"
}

// blobPath resolves a Nexus blob_ref to the .bytes file.
func (im *Importer) blobPath(ref string) (string, error) {
	// default@<uuid>@2026-09-10T16:06   (date-based layout, 3.7x+)
	// default@<nodeid>:<uuid>           (vol/chap layout, older)
	parts := strings.Split(ref, "@")
	if len(parts) < 2 {
		return "", fmt.Errorf("bad blob ref %q", ref)
	}
	store := parts[0]
	id := parts[1]
	base := filepath.Join(im.Opt.BlobsDir, store, "content")
	if len(parts) == 3 {
		t, err := time.Parse("2006-01-02T15:04", parts[2])
		if err == nil {
			p := filepath.Join(base, t.Format("2006/01/02/15/04"), id+".bytes")
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	if i := strings.IndexByte(id, ':'); i >= 0 {
		id = id[i+1:]
	}
	// Legacy volume/chapter layout: derived from Java String.hashCode().
	h := javaHash(id)
	abs := func(x int32) int32 {
		if x < 0 {
			return -x
		}
		return x
	}
	p := filepath.Join(base, fmt.Sprintf("vol-%02d", abs(h%43)+1), fmt.Sprintf("chap-%02d", abs(h%47)+1), id+".bytes")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	// Last resort: search.
	var found string
	filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == id+".bytes" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found != "" {
		return found, nil
	}
	return "", fmt.Errorf("blob file for %s not found", ref)
}

func javaHash(s string) int32 {
	var h int32
	for _, c := range s {
		h = 31*h + int32(c)
	}
	return h
}

func (im *Importer) importAsset(ctx context.Context, f string, rp *model.Repository, a *nasset) error {
	path := strings.TrimPrefix(a.path, "/")
	immutable := false
	var pkg *model.Package
	switch f {
	case "maven2":
		immutable = a.kind == "ARTIFACT" && !strings.Contains(path, "-SNAPSHOT/") || strings.Contains(path, "-SNAPSHOT/") && regexp.MustCompile(`\d{8}\.\d{6}-\d+`).MatchString(path)
		if a.hasComp && a.kind == "ARTIFACT" {
			v := a.version
			if a.baseVer != nil && *a.baseVer != "" {
				v = *a.baseVer
			}
			attrs, _ := json.Marshal(map[string]any{"groupId": a.ns, "artifactId": a.name, "baseVersion": v})
			pkg = &model.Package{Namespace: a.ns, Name: a.name, Version: v, Attrs: attrs}
		}
	case "npm":
		immutable = a.kind == "TARBALL"
		if a.hasComp && a.kind == "TARBALL" {
			full := a.name
			if a.ns != "" {
				full = a.ns + "/" + a.name
			}
			attrs, _ := json.Marshal(map[string]any{"name": full})
			pkg = &model.Package{Namespace: a.ns, Name: a.name, Version: a.version, Attrs: attrs}
		}
	case "docker":
		path = strings.TrimPrefix(path, "v2/")
		if strings.HasPrefix(path, "-/blobs/") {
			// Repository-wide blob: store the bytes; per-image assets are
			// created from manifests afterwards.
			return im.storeBlob(ctx, rp, a, nil)
		}
		immutable = strings.Contains(path, "/manifests/sha256:")
		if a.hasComp && a.kind == "MANIFEST" && !immutable {
			ns, short := "", a.name
			if i := strings.LastIndexByte(a.name, '/'); i > 0 {
				ns, short = a.name[:i], a.name[i+1:]
			}
			attrs, _ := json.Marshal(map[string]any{"image": a.name})
			pkg = &model.Package{Namespace: ns, Name: short, Version: a.version, Attrs: attrs}
		}
	default:
		immutable = true
	}
	digest, err := im.storeBlobBytes(ctx, rp, a)
	if err != nil {
		return err
	}
	if im.Opt.DryRun {
		return nil
	}
	asset := &model.Asset{RepoID: rp.ID, Path: path, Size: a.size, ContentType: a.ctype, LastDownloadedAt: a.lastDl}
	d := string(digest)
	asset.BlobDigest = &d
	if rp.Type == model.Proxy && !immutable {
		now := time.Now()
		asset.CacheExpiresAt = &now // revalidate on first access
	}
	if pkg != nil {
		pkg.RepoID = rp.ID
		if err := im.Content.UpsertPackage(ctx, pkg); err != nil {
			return err
		}
		asset.PackageID = &pkg.ID
	}
	if err := im.Content.UpsertAsset(ctx, asset); err != nil {
		return err
	}
	im.count("assets")
	return nil
}

func (im *Importer) storeBlob(ctx context.Context, rp *model.Repository, a *nasset, _ any) error {
	_, err := im.storeBlobBytes(ctx, rp, a)
	if err == nil {
		im.count("blobs")
	}
	return err
}

// storeBlobBytes links/copies the Nexus blob into our storage and records it.
func (im *Importer) storeBlobBytes(ctx context.Context, rp *model.Repository, a *nasset) (storage.Digest, error) {
	src, err := im.blobPath(a.blobRef)
	if err != nil {
		return "", err
	}
	// Respect soft deletes.
	if props, err := os.ReadFile(strings.TrimSuffix(src, ".bytes") + ".properties"); err == nil && strings.Contains(string(props), "deleted=true") {
		return "", errors.New("blob is soft-deleted in nexus")
	}
	var digest storage.Digest
	if h := a.checksums["sha256"]; len(h) == 64 {
		digest = storage.DigestFromHex(h)
	}
	if im.Opt.DryRun {
		return digest, nil
	}
	st, err := im.Content.Store(rp.StorageID)
	if err != nil {
		return "", err
	}
	if digest == "" || !im.Opt.Link {
		f, err := os.Open(src)
		if err != nil {
			return "", err
		}
		defer f.Close()
		info, err := st.Put(ctx, f, digest)
		if err != nil {
			return "", err
		}
		return info.Digest, im.Content.RecordBlob(ctx, rp.StorageID, info)
	}
	if err := st.Link(ctx, src, digest); err != nil {
		return "", err
	}
	return digest, im.Content.RecordBlob(ctx, rp.StorageID, storage.Info{Digest: digest, Size: a.size})
}

// linkDockerBlobs creates <image>/blobs/<digest> assets for every layer and
// config referenced by imported manifests, so pulls resolve locally.
func (im *Importer) linkDockerBlobs(ctx context.Context) error {
	for _, rp := range im.repos {
		if rp.Format != "docker" {
			continue
		}
		assets, err := im.Content.ListAssets(ctx, rp.ID, "", 1000000)
		if err != nil {
			return err
		}
		for _, a := range assets {
			i := strings.Index(a.Path, "/manifests/")
			if i <= 0 || a.BlobDigest == nil {
				continue
			}
			name := a.Path[:i]
			rc, _, err := im.Content.OpenBlob(ctx, storage.Digest(*a.BlobDigest))
			if err != nil {
				continue
			}
			var m struct {
				Config *struct {
					Digest string `json:"digest"`
				} `json:"config"`
				Layers []struct {
					Digest string `json:"digest"`
				} `json:"layers"`
			}
			json.NewDecoder(rc).Decode(&m)
			rc.Close()
			var refs []string
			if m.Config != nil {
				refs = append(refs, m.Config.Digest)
			}
			for _, l := range m.Layers {
				refs = append(refs, l.Digest)
			}
			for _, d := range refs {
				size, ok, err := im.Content.BlobExists(ctx, storage.Digest(d))
				if err != nil || !ok {
					continue
				}
				ds := d
				im.Content.UpsertAsset(ctx, &model.Asset{RepoID: rp.ID, Path: name + "/blobs/" + d, BlobDigest: &ds, Size: size, ContentType: "application/octet-stream"})
				im.count("docker.blob_assets")
			}
		}
	}
	return nil
}
