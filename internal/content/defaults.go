package content

import (
	"context"
	"encoding/json"

	"github.com/holiaokho/holiaokho/internal/model"
)

// defaultRepo is the starter set created on a fresh install: the same
// repositories Nexus Repository creates (maven + nuget), plus npm and Docker
// proxies, which almost every team wants on day one.
type defaultRepo struct {
	Name   string
	Format string
	Type   model.RepoType
	Attrs  map[string]any
}

func defaultRepos() []defaultRepo {
	proxy := func(url string) map[string]any {
		return map[string]any{"proxy": map[string]any{"remoteUrl": url, "contentMaxAge": 1440, "metadataMaxAge": 1440, "negativeCacheTtl": 1440, "autoBlock": true}}
	}
	merge := func(a, b map[string]any) map[string]any {
		for k, v := range b {
			a[k] = v
		}
		return a
	}
	return []defaultRepo{
		{"maven-central", "maven", model.Proxy, merge(proxy("https://repo1.maven.org/maven2/"),
			map[string]any{"maven": map[string]any{"layoutPolicy": "STRICT", "versionPolicy": "RELEASE"}})},
		{"maven-releases", "maven", model.Hosted, map[string]any{
			"hosted": map[string]any{"writePolicy": "allow_once"},
			"maven":  map[string]any{"layoutPolicy": "STRICT", "versionPolicy": "RELEASE"}}},
		{"maven-snapshots", "maven", model.Hosted, map[string]any{
			"hosted": map[string]any{"writePolicy": "allow"},
			"maven":  map[string]any{"layoutPolicy": "STRICT", "versionPolicy": "SNAPSHOT"}}},
		{"maven-public", "maven", model.Group, map[string]any{
			"group": map[string]any{"members": []string{"maven-releases", "maven-snapshots", "maven-central"}},
			"maven": map[string]any{"layoutPolicy": "STRICT", "versionPolicy": "MIXED"}}},
		{"npm-proxy", "npm", model.Proxy, proxy("https://registry.npmjs.org/")},
		{"npm-hosted", "npm", model.Hosted, map[string]any{"hosted": map[string]any{"writePolicy": "allow_once"}}},
		{"npm-group", "npm", model.Group, map[string]any{
			"group": map[string]any{"members": []string{"npm-hosted", "npm-proxy"}}}},
		{"docker-hub", "docker", model.Proxy, merge(proxy("https://registry-1.docker.io"),
			map[string]any{"docker": map[string]any{"indexType": "HUB", "pathEnabled": true}})},
		// GitHub Container Registry is where a lot of CI tooling now lives, and
		// image names there are always owner/name, so REGISTRY rather than HUB:
		// HUB would rewrite a single-segment name to library/<name>.
		{"ghcr", "docker", model.Proxy, merge(proxy("https://ghcr.io"),
			map[string]any{"docker": map[string]any{"indexType": "REGISTRY", "pathEnabled": true}})},
		// The group owns the port rather than docker-hub, because a Docker
		// daemon can only be pointed at one plain-HTTP address: whatever holds
		// 8082 has to serve both the Hub mirror and anything else you proxy.
		//
		// Binding it by default is safe — a port already in use logs an error
		// and the connector is skipped, the server keeps running — and 8082 is
		// the port Nexus uses, so an existing daemon.json keeps working.
		{"docker-group", "docker", model.Group, map[string]any{
			"group":  map[string]any{"members": []string{"docker-hub", "ghcr"}},
			"docker": map[string]any{"indexType": "HUB", "pathEnabled": true, "httpPort": 8082}}},
		{"nuget.org-proxy", "nuget", model.Proxy, proxy("https://api.nuget.org/v3/index.json")},
		{"nuget-hosted", "nuget", model.Hosted, map[string]any{"hosted": map[string]any{"writePolicy": "allow_once"}}},
		{"nuget-group", "nuget", model.Group, map[string]any{
			"group": map[string]any{"members": []string{"nuget-hosted", "nuget.org-proxy"}}}},
	}
}

// EnsureDefaultRepos creates the Nexus-equivalent starter repositories, but
// only on a completely empty instance so it never surprises an existing one.
func (s *Service) EnsureDefaultRepos(ctx context.Context) error {
	var n int
	if err := s.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM repositories`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	st, err := s.StorageByName(ctx, "default")
	if err != nil {
		return err
	}
	for _, d := range defaultRepos() {
		attrs, err := json.Marshal(d.Attrs)
		if err != nil {
			return err
		}
		rp := &model.Repository{Name: d.Name, Format: d.Format, Type: d.Type, StorageID: st.ID, Online: true, Attributes: attrs}
		if err := s.CreateRepo(ctx, rp); err != nil {
			s.Log.Warn("create default repository", "name", d.Name, "err", err)
			continue
		}
		s.Log.Info("created default repository", "name", d.Name, "format", d.Format, "type", d.Type)
	}
	return nil
}
