// Package maven implements the Maven 2 repository format: hosted deploys
// (mvn deploy), proxying of remote repositories (Central) and group
// aggregation with maven-metadata.xml merging.
package maven

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "maven"

const challenge = `Basic realm="Holiaokho Maven"`

// Attrs is the "maven" block of repository attributes.
type Attrs struct {
	LayoutPolicy  string `json:"layoutPolicy"`  // STRICT | PERMISSIVE
	VersionPolicy string `json:"versionPolicy"` // RELEASE | SNAPSHOT | MIXED
}

func attrsOf(r *model.Repository) Attrs {
	a := Attrs{LayoutPolicy: "STRICT", VersionPolicy: "MIXED"}
	format.AttrBlock(r, "maven", &a)
	if a.LayoutPolicy == "" {
		a.LayoutPolicy = "STRICT"
	}
	if a.VersionPolicy == "" {
		a.VersionPolicy = "MIXED"
	}
	return a
}

type Format struct{}

func (Format) Name() string { return Name }

func (Format) VersionLess(a, b string) bool { return Less(a, b) }

func (Format) Parse(p string) *model.Package {
	c := ParsePath(p)
	if c == nil || c.Checksum != "" || c.Signature {
		return nil
	}
	return packageFor(c)
}

func packageFor(c *Coordinates) *model.Package {
	attrs, _ := json.Marshal(map[string]any{"groupId": c.GroupID, "artifactId": c.ArtifactID, "baseVersion": c.BaseVersion})
	return &model.Package{Namespace: c.GroupID, Name: c.ArtifactID, Version: c.BaseVersion, Attrs: attrs}
}

func (Format) ValidateAttributes(r *model.Repository, attrs map[string]json.RawMessage) error {
	a := Attrs{}
	if raw, ok := attrs["maven"]; ok {
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("maven attributes: %w", err)
		}
	}
	a.LayoutPolicy = strings.ToUpper(a.LayoutPolicy)
	a.VersionPolicy = strings.ToUpper(a.VersionPolicy)
	if a.LayoutPolicy == "" {
		a.LayoutPolicy = "STRICT"
	}
	if a.VersionPolicy == "" {
		if r.Type == model.Hosted {
			a.VersionPolicy = "RELEASE"
		} else {
			a.VersionPolicy = "MIXED"
		}
	}
	switch a.LayoutPolicy {
	case "STRICT", "PERMISSIVE":
	default:
		return errors.New("maven.layoutPolicy must be STRICT or PERMISSIVE")
	}
	switch a.VersionPolicy {
	case "RELEASE", "SNAPSHOT", "MIXED":
	default:
		return errors.New("maven.versionPolicy must be RELEASE, SNAPSHOT or MIXED")
	}
	raw, _ := json.Marshal(a)
	attrs["maven"] = raw
	return nil
}

type handler struct {
	repo *model.Repository
	d    format.Deps
	a    Attrs
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	return &handler{repo: r, d: d, a: attrsOf(r)}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		if p == "" || strings.HasSuffix(p, "/") {
			h.listing(w, r, p)
			return
		}
		h.get(w, r, p)
	case http.MethodPut:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.put(w, r, p)
	case http.MethodDelete:
		if !format.Authorize(w, r, h.repo, auth.Delete, challenge) {
			return
		}
		h.delete(w, r, p)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		format.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

// allowedByVersionPolicy filters snapshot vs release paths.
func (h *handler) allowedByVersionPolicy(p string) bool {
	snap := IsSnapshotPath(p)
	switch h.a.VersionPolicy {
	case "RELEASE":
		return !snap
	case "SNAPSHOT":
		return snap
	}
	return true
}

func (h *handler) get(w http.ResponseWriter, r *http.Request, p string) {
	p, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	if IsMetadata(p) {
		h.getMetadata(w, r, p)
		return
	}
	if !h.allowedByVersionPolicy(p) {
		format.WriteError(w, http.StatusNotFound, "not_found", "version policy does not allow this path")
		return
	}
	pol := repo.Policy{Kind: repo.Content, ContentType: ContentType(p)}
	c := ParsePath(p)
	if c != nil {
		if c.Snapshot && c.Timestamp == "" {
			// artifact-1.0-SNAPSHOT.jar (non-timestamped) is mutable.
			pol.Kind = repo.Metadata
		} else {
			pol.Immutable = true
		}
		if c.Checksum == "" && !c.Signature {
			pol.Package = packageFor(c)
		}
	} else if h.a.LayoutPolicy == "PERMISSIVE" {
		pol.Kind = repo.Metadata
	}
	format.FetchAndServe(w, r, h.d, h.repo, p, pol)
}

// ------------------------------------------------------------- metadata

func (h *handler) getMetadata(w http.ResponseWriter, r *http.Request, p string) {
	pol := repo.Policy{Kind: repo.Metadata, ContentType: ContentType(p)}
	switch h.repo.Type {
	case model.Group:
		h.groupMetadata(w, r, p, pol)
	case model.Hosted:
		res, err := h.d.Engine.Fetch(r.Context(), h.repo, p, pol)
		if errors.Is(err, repo.ErrNotFound) {
			// Generate on demand so API-uploaded artifacts resolve.
			b, ok := h.generateHosted(r.Context(), p)
			if !ok {
				format.WriteError(w, http.StatusNotFound, "not_found", "not found")
				return
			}
			format.ServeBytes(w, r, ContentType(p), b, time.Time{})
			return
		}
		if err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		format.ServeResult(w, r, h.d, res)
	default:
		format.FetchAndServe(w, r, h.d, h.repo, p, pol)
	}
}

// generateHosted builds metadata (or its checksum) from the database.
func (h *handler) generateHosted(ctx context.Context, p string) ([]byte, bool) {
	base := p
	var sum string
	if IsChecksum(p) {
		sum = strings.TrimPrefix(path.Ext(p), ".")
		base = strings.TrimSuffix(p, path.Ext(p))
	}
	if path.Base(base) != metadataName {
		return nil, false
	}
	m, err := h.buildMetadata(ctx, path.Dir(base))
	if err != nil || m == nil {
		return nil, false
	}
	b := m.Marshal()
	if sum != "" {
		return []byte(Checksum(sum, b)), true
	}
	return b, true
}

// buildMetadata generates the metadata for a directory that is either
// group/artifact or group/artifact/version-SNAPSHOT.
func (h *handler) buildMetadata(ctx context.Context, dir string) (*Metadata, error) {
	segs := strings.Split(dir, "/")
	if len(segs) < 2 {
		return nil, nil
	}
	if strings.HasSuffix(segs[len(segs)-1], "-SNAPSHOT") && len(segs) >= 3 {
		version := segs[len(segs)-1]
		artifact := segs[len(segs)-2]
		group := strings.Join(segs[:len(segs)-2], ".")
		assets, err := h.d.Content.ListAssets(ctx, h.repo.ID, dir+"/", 5000)
		if err != nil {
			return nil, err
		}
		var files []*Coordinates
		var latest time.Time
		for _, a := range assets {
			if c := ParsePath(a.Path); c != nil {
				files = append(files, c)
				if a.UpdatedAt.After(latest) {
					latest = a.UpdatedAt
				}
			}
		}
		if len(files) == 0 {
			return nil, nil
		}
		return BuildSnapshot(group, artifact, version, files, latest), nil
	}
	artifact := segs[len(segs)-1]
	group := strings.Join(segs[:len(segs)-1], ".")
	pkgs, err := h.d.Content.PackageVersions(ctx, h.repo.ID, group, artifact)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, nil
	}
	var versions []string
	var latest time.Time
	for _, pk := range pkgs {
		versions = append(versions, pk.Version)
		if pk.UpdatedAt.After(latest) {
			latest = pk.UpdatedAt
		}
	}
	return BuildGA(group, artifact, versions, latest), nil
}

// RebuildMetadata regenerates and stores maven-metadata.xml (+ checksums)
// for a hosted repository directory. Used after deletions and by the
// repair task.
func RebuildMetadata(ctx context.Context, d format.Deps, rp *model.Repository, dir string) error {
	h := &handler{repo: rp, d: d, a: attrsOf(rp)}
	m, err := h.buildMetadata(ctx, dir)
	if err != nil {
		return err
	}
	mp := path.Join(dir, metadataName)
	if m == nil {
		for _, ext := range []string{"", ".sha1", ".md5", ".sha256", ".sha512"} {
			d.Engine.Delete(ctx, rp, mp+ext)
		}
		return nil
	}
	b := m.Marshal()
	if _, err := d.Engine.Put(ctx, rp, mp, bytes.NewReader(b), repo.PutOptions{ContentType: "application/xml", AllowRedeploy: true}); err != nil {
		return err
	}
	for _, ext := range []string{"sha1", "md5", "sha256", "sha512"} {
		if _, err := d.Engine.Put(ctx, rp, mp+"."+ext, strings.NewReader(Checksum(ext, b)), repo.PutOptions{ContentType: "text/plain", AllowRedeploy: true}); err != nil {
			return err
		}
	}
	return nil
}

func (h *handler) groupMetadata(w http.ResponseWriter, r *http.Request, p string, pol repo.Policy) {
	base := p
	var sum string
	if IsChecksum(p) {
		sum = strings.TrimPrefix(path.Ext(p), ".")
		base = strings.TrimSuffix(p, path.Ext(p))
	}
	if strings.HasSuffix(base, ".asc") {
		// Signatures of merged metadata cannot be produced; first-match.
		format.FetchAndServe(w, r, h.d, h.repo, p, pol)
		return
	}
	members, _ := h.d.Engine.Members(r.Context(), h.repo)
	var docs []*Metadata
	var latest time.Time
	for _, m := range members {
		mh := &handler{repo: m, d: h.d, a: attrsOf(m)}
		if !mh.allowedByVersionPolicy(base) && strings.HasSuffix(path.Dir(base), "-SNAPSHOT") {
			continue
		}
		b, res, err := h.d.Engine.ReadAll(r.Context(), m, base, pol, 8<<20)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) && m.Type == model.Hosted {
				if gb, ok := mh.generateHosted(r.Context(), base); ok {
					b = gb
				} else {
					continue
				}
			} else {
				if !errors.Is(err, repo.ErrNotFound) {
					h.d.Log.Warn("group member metadata", "group", h.repo.Name, "member", m.Name, "path", base, "err", err)
				}
				continue
			}
		}
		if res != nil && res.Asset != nil && res.Asset.UpdatedAt.After(latest) {
			latest = res.Asset.UpdatedAt
		}
		md, err := ParseMetadata(b)
		if err != nil {
			h.d.Log.Warn("bad member metadata", "member", m.Name, "path", base, "err", err)
			continue
		}
		docs = append(docs, md)
	}
	if len(docs) == 0 {
		format.WriteError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	out := Merge(docs).Marshal()
	if sum != "" {
		format.ServeBytes(w, r, "text/plain", []byte(Checksum(sum, out)), latest)
		return
	}
	format.ServeBytes(w, r, "application/xml", out, latest)
}

// ------------------------------------------------------------------- put

func (h *handler) put(w http.ResponseWriter, r *http.Request, p string) {
	p, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	if h.repo.Type != model.Hosted {
		format.WriteError(w, http.StatusBadRequest, "repo.read_only", "only hosted repositories accept deployments")
		return
	}
	opt := repo.PutOptions{ContentType: ContentType(p)}
	c := ParsePath(p)
	switch {
	case IsMetadata(p):
		opt.AllowRedeploy = true
	case c != nil:
		if !h.allowedByVersionPolicy(p) {
			format.WriteError(w, http.StatusBadRequest, "maven.version_policy", "version policy %s does not allow this artifact", h.a.VersionPolicy)
			return
		}
		if c.Snapshot || c.Checksum != "" || c.Signature {
			opt.AllowRedeploy = true
		}
		if c.Checksum == "" && !c.Signature {
			opt.Package = packageFor(c)
			opt.Attrs = map[string]any{"classifier": c.Classifier, "extension": c.Extension}
		}
	default:
		if h.a.LayoutPolicy == "STRICT" {
			format.WriteError(w, http.StatusBadRequest, "maven.layout", "path does not follow the Maven 2 layout")
			return
		}
		opt.AllowRedeploy = true
	}
	a, err := h.d.Engine.Put(r.Context(), h.repo, p, r.Body, opt)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	w.Header().Set("Location", r.URL.Path)
	w.Header().Set("X-Checksum-Sha256", strings.TrimPrefix(*a.BlobDigest, "sha256:"))
	w.WriteHeader(http.StatusCreated)
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request, p string) {
	p, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	if err := h.d.Engine.Delete(r.Context(), h.repo, p); err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- listing

func (h *handler) listing(w http.ResponseWriter, r *http.Request, dir string) {
	dirs := map[string]bool{}
	files := map[string]*model.Asset{}
	var repos []*model.Repository
	if h.repo.Type == model.Group {
		repos, _ = h.d.Engine.Members(r.Context(), h.repo)
	} else {
		repos = []*model.Repository{h.repo}
	}
	for _, rp := range repos {
		ds, fs, err := h.d.Content.ListChildren(r.Context(), rp.ID, dir)
		if err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		for _, d := range ds {
			dirs[d] = true
		}
		for _, f := range fs {
			if _, ok := files[path.Base(f.Path)]; !ok {
				files[path.Base(f.Path)] = f
			}
		}
	}
	if len(dirs) == 0 && len(files) == 0 && dir != "" {
		format.WriteError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	var dn, fn []string
	for d := range dirs {
		dn = append(dn, d)
	}
	for f := range files {
		fn = append(fn, f)
	}
	sort.Strings(dn)
	sort.Strings(fn)
	var b strings.Builder
	title := "/" + dir
	fmt.Fprintf(&b, "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>Index of %s</title></head><body><h1>Index of %s</h1><pre>\n", html.EscapeString(title), html.EscapeString(title))
	if dir != "" {
		b.WriteString("<a href=\"../\">../</a>\n")
	}
	for _, d := range dn {
		fmt.Fprintf(&b, "<a href=\"%s/\">%s/</a>\n", html.EscapeString(d), html.EscapeString(d))
	}
	for _, f := range fn {
		a := files[f]
		fmt.Fprintf(&b, "<a href=\"%s\">%s</a>%s%s %12d\n", html.EscapeString(f), html.EscapeString(f),
			strings.Repeat(" ", max(1, 60-len(f))), a.UpdatedAt.UTC().Format("2006-01-02 15:04"), a.Size)
	}
	b.WriteString("</pre></body></html>\n")
	format.ServeBytes(w, r, "text/html; charset=utf-8", []byte(b.String()), time.Time{})
}

var _ io.Reader = (*bytes.Reader)(nil)
