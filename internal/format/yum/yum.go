// Package yum implements RPM repositories: proxy mirrors (repodata/ with
// short TTL, packages immutable) and hosted repositories that accept .rpm
// uploads and publish createrepo-compatible repodata (primary, filelists,
// other, repomd.xml, optionally signed).
package yum

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/common"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/pkgsign"
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/storage"
)

const Name = "yum"

type Attrs struct {
	// RepodataDepth is where repodata/ lives relative to the root (0 = root).
	RepodataDepth int    `json:"repodataDepth"`
	SigningKey    string `json:"signingKey,omitempty"`
	Passphrase    string `json:"passphrase,omitempty"`
}

func attrsOf(r *model.Repository) Attrs {
	var a Attrs
	format.AttrBlock(r, "yum", &a)
	return a
}

type Format struct{}

func (Format) Name() string { return Name }

func (Format) ValidateAttributes(r *model.Repository, attrs map[string]json.RawMessage) error {
	var a Attrs
	if raw, ok := attrs["yum"]; ok {
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("yum attributes: %w", err)
		}
	}
	if a.SigningKey != "" {
		if _, err := pkgsign.LoadSigner(a.SigningKey, a.Passphrase); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(a)
	attrs["yum"] = raw
	return nil
}

func (Format) VersionLess(a, b string) bool { return rpmVerLess(a, b) }

func (Format) Parse(p string) *model.Package {
	if !strings.HasSuffix(p, ".rpm") {
		return nil
	}
	base := strings.TrimSuffix(path.Base(p), ".rpm")
	// name-version-release.arch
	i := strings.LastIndexByte(base, '.')
	if i < 0 {
		return nil
	}
	arch := base[i+1:]
	nvr := base[:i]
	j := strings.LastIndexByte(nvr, '-')
	if j < 0 {
		return nil
	}
	k := strings.LastIndexByte(nvr[:j], '-')
	if k < 0 {
		return nil
	}
	return &model.Package{Name: nvr[:k], Version: nvr[k+1:j] + "-" + nvr[j+1:], Namespace: arch}
}

type handler struct {
	repo *model.Repository
	d    format.Deps
	a    Attrs
	m    *common.Mirror
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	h := &handler{repo: r, d: d, a: attrsOf(r)}
	h.m = &common.Mirror{Repo: r, Deps: d, Challenge: `Basic realm="Holiaokho YUM"`,
		IsMetadata: func(p string) bool { return strings.Contains(p, "repodata/") || !strings.HasSuffix(p, ".rpm") },
		ContentType: func(p string) string {
			if strings.HasSuffix(p, ".rpm") {
				return "application/x-rpm"
			}
			return common.ContentTypeByExt(p)
		},
		Parse:    Format{}.Parse,
		Generate: h.generate,
		OnDelete: func(ctx context.Context, p string) { h.rebuild(ctx) },
	}
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	if h.repo.Type == model.Hosted && (r.Method == http.MethodPost || r.Method == http.MethodPut) && strings.HasSuffix(p, ".rpm") || (h.repo.Type == model.Hosted && r.Method == http.MethodPost && p == "") {
		if !format.Authorize(w, r, h.repo, auth.Write, h.m.Challenge) {
			return
		}
		h.upload(w, r, p)
		return
	}
	if p == "repository-key.gpg" || p == "RPM-GPG-KEY" {
		if h.a.SigningKey == "" {
			format.WriteError(w, http.StatusNotFound, "not_found", "repository is not signed")
			return
		}
		s, err := pkgsign.LoadSigner(h.a.SigningKey, h.a.Passphrase)
		if err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		pub, _ := s.PublicKey()
		format.ServeBytes(w, r, "application/pgp-keys", []byte(pub), time.Time{})
		return
	}
	h.m.Serve(w, r)
}

func (h *handler) upload(w http.ResponseWriter, r *http.Request, p string) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 2<<30))
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "body.invalid", "read body")
		return
	}
	pkg, err := ReadRPM(bytes.NewReader(data))
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "yum.invalid", "not an rpm: %s", err.Error())
		return
	}
	if p == "" || !strings.Contains(p, "/") {
		p = fmt.Sprintf("Packages/%s/%s-%s-%s.%s.rpm", strings.ToLower(pkg.Name[:1]), pkg.Name, pkg.Version, pkg.Release, pkg.Arch)
	}
	cp, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	meta, _ := json.Marshal(pkg)
	attrs := map[string]any{"rpm": json.RawMessage(meta)}
	mp := &model.Package{Name: pkg.Name, Version: pkg.Version + "-" + pkg.Release, Namespace: pkg.Arch, Attrs: meta}
	if _, err := h.d.Engine.Put(r.Context(), h.repo, cp, bytes.NewReader(data), repo.PutOptions{ContentType: "application/x-rpm", Package: mp, Attrs: attrs}); err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	h.rebuild(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"path": cp, "name": pkg.Name, "version": pkg.Version, "release": pkg.Release, "arch": pkg.Arch})
}

// ---------------------------------------------------------------- repodata

func (h *handler) generate(ctx context.Context, rp *model.Repository, p string) ([]byte, bool) {
	if rp.Type != model.Hosted || !strings.HasPrefix(p, "repodata/") {
		return nil, false
	}
	files, err := h.build(ctx, rp)
	if err != nil {
		return nil, false
	}
	b, ok := files[p]
	return b, ok
}

func (h *handler) rebuild(ctx context.Context) {
	if h.repo.Type != model.Hosted {
		return
	}
	files, err := h.build(ctx, h.repo)
	if err != nil {
		h.d.Log.Warn("yum rebuild", "repo", h.repo.Name, "err", err)
		return
	}
	// Remove stale hash-named repodata files, then write the new set.
	old, _ := h.d.Content.ListAssets(ctx, h.repo.ID, "repodata/", 1000)
	for _, a := range old {
		if _, keep := files[a.Path]; !keep {
			h.d.Engine.Delete(ctx, h.repo, a.Path)
		}
	}
	for p, b := range files {
		h.d.Engine.Put(ctx, h.repo, p, bytes.NewReader(b), repo.PutOptions{ContentType: common.ContentTypeByExt(p), AllowRedeploy: true})
	}
}

type pkgEntry struct {
	asset *model.Asset
	pkg   *Package
}

func (h *handler) packages(ctx context.Context, rp *model.Repository) ([]pkgEntry, error) {
	assets, err := h.d.Content.ListAssets(ctx, rp.ID, "", 100000)
	if err != nil {
		return nil, err
	}
	var out []pkgEntry
	for _, a := range assets {
		if !strings.HasSuffix(a.Path, ".rpm") || a.BlobDigest == nil {
			continue
		}
		var attrs struct {
			RPM *Package `json:"rpm"`
		}
		json.Unmarshal(a.Attrs, &attrs)
		if attrs.RPM == nil {
			rc, _, err := h.d.Content.OpenBlob(ctx, storage.Digest(*a.BlobDigest))
			if err != nil {
				continue
			}
			pkg, err := ReadRPM(rc)
			rc.Close()
			if err != nil {
				continue
			}
			attrs.RPM = pkg
			meta, _ := json.Marshal(pkg)
			a.Attrs, _ = json.Marshal(map[string]any{"rpm": json.RawMessage(meta)})
			h.d.Content.UpsertAsset(ctx, a)
		}
		out = append(out, pkgEntry{a, attrs.RPM})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].asset.Path < out[j].asset.Path })
	return out, nil
}

func esc(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func depsXML(tag string, deps []Dep) string {
	if len(deps) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "      <rpm:%s>\n", tag)
	for _, d := range deps {
		fmt.Fprintf(&b, `        <rpm:entry name="%s"`, esc(d.Name))
		if d.Flags != "" {
			fmt.Fprintf(&b, ` flags="%s" epoch="%s" ver="%s"`, d.Flags, d.Epoch, esc(d.Version))
			if d.Release != "" {
				fmt.Fprintf(&b, ` rel="%s"`, esc(d.Release))
			}
		}
		if d.Pre && tag == "requires" {
			b.WriteString(` pre="1"`)
		}
		b.WriteString("/>\n")
	}
	fmt.Fprintf(&b, "      </rpm:%s>\n", tag)
	return b.String()
}

func (h *handler) build(ctx context.Context, rp *model.Repository) (map[string][]byte, error) {
	pkgs, err := h.packages(ctx, rp)
	if err != nil {
		return nil, err
	}
	var primary, filelists, other strings.Builder
	fmt.Fprintf(&primary, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="%d">`+"\n", len(pkgs))
	fmt.Fprintf(&filelists, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<filelists xmlns="http://linux.duke.edu/metadata/filelists" packages="%d">`+"\n", len(pkgs))
	fmt.Fprintf(&other, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<otherdata xmlns="http://linux.duke.edu/metadata/other" packages="%d">`+"\n", len(pkgs))
	for _, e := range pkgs {
		p := e.pkg
		sum := strings.TrimPrefix(*e.asset.BlobDigest, "sha256:")
		fmt.Fprintf(&primary, "<package type=\"rpm\">\n  <name>%s</name>\n  <arch>%s</arch>\n  <version epoch=\"%s\" ver=\"%s\" rel=\"%s\"/>\n  <checksum type=\"sha256\" pkgid=\"YES\">%s</checksum>\n  <summary>%s</summary>\n  <description>%s</description>\n  <packager>%s</packager>\n  <url>%s</url>\n  <time file=\"%d\" build=\"%d\"/>\n  <size package=\"%d\" installed=\"%d\" archive=\"%d\"/>\n  <location href=\"%s\"/>\n  <format>\n    <rpm:license>%s</rpm:license>\n    <rpm:vendor>%s</rpm:vendor>\n    <rpm:group>%s</rpm:group>\n    <rpm:buildhost>%s</rpm:buildhost>\n    <rpm:sourcerpm>%s</rpm:sourcerpm>\n    <rpm:header-range start=\"%d\" end=\"%d\"/>\n",
			esc(p.Name), esc(p.Arch), p.Epoch, esc(p.Version), esc(p.Release), sum, esc(p.Summary), esc(p.Description), esc(p.Packager), esc(p.URL),
			e.asset.UpdatedAt.Unix(), p.BuildTime, e.asset.Size, p.InstalledSize, p.InstalledSize, esc(e.asset.Path),
			esc(p.License), esc(p.Vendor), esc(p.Group), esc(p.BuildHost), esc(p.SourceRPM), p.HeaderStart, p.HeaderEnd)
		primary.WriteString(depsXML("provides", p.Provides))
		primary.WriteString(depsXML("requires", p.Requires))
		primary.WriteString(depsXML("conflicts", p.Conflicts))
		primary.WriteString(depsXML("obsoletes", p.Obsoletes))
		// Primary lists only "primary" files (bin dirs, /etc) per createrepo.
		for _, f := range p.Files {
			if strings.HasPrefix(f, "/etc/") || strings.Contains(f, "bin/") {
				fmt.Fprintf(&primary, "    <file>%s</file>\n", esc(f))
			}
		}
		primary.WriteString("  </format>\n</package>\n")
		fmt.Fprintf(&filelists, "<package pkgid=\"%s\" name=\"%s\" arch=\"%s\">\n  <version epoch=\"%s\" ver=\"%s\" rel=\"%s\"/>\n", sum, esc(p.Name), esc(p.Arch), p.Epoch, esc(p.Version), esc(p.Release))
		for _, f := range p.Files {
			fmt.Fprintf(&filelists, "  <file>%s</file>\n", esc(f))
		}
		for _, d := range p.Dirs {
			fmt.Fprintf(&filelists, "  <file type=\"dir\">%s</file>\n", esc(d))
		}
		filelists.WriteString("</package>\n")
		fmt.Fprintf(&other, "<package pkgid=\"%s\" name=\"%s\" arch=\"%s\">\n  <version epoch=\"%s\" ver=\"%s\" rel=\"%s\"/>\n</package>\n", sum, esc(p.Name), esc(p.Arch), p.Epoch, esc(p.Version), esc(p.Release))
	}
	primary.WriteString("</metadata>\n")
	filelists.WriteString("</filelists>\n")
	other.WriteString("</otherdata>\n")

	out := map[string][]byte{}
	now := time.Now().Unix()
	var repomd strings.Builder
	repomd.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<repomd xmlns="http://linux.duke.edu/metadata/repo" xmlns:rpm="http://linux.duke.edu/metadata/rpm">` + "\n")
	fmt.Fprintf(&repomd, "  <revision>%d</revision>\n", now)
	for _, item := range []struct{ typ, body string }{{"primary", primary.String()}, {"filelists", filelists.String()}, {"other", other.String()}} {
		open := []byte(item.body)
		var gz bytes.Buffer
		zw := gzip.NewWriter(&gz)
		zw.Write(open)
		zw.Close()
		osum := sha256.Sum256(open)
		gsum := sha256.Sum256(gz.Bytes())
		name := fmt.Sprintf("repodata/%s-%s.xml.gz", hex.EncodeToString(gsum[:]), item.typ)
		out[name] = gz.Bytes()
		fmt.Fprintf(&repomd, "  <data type=\"%s\">\n    <checksum type=\"sha256\">%s</checksum>\n    <open-checksum type=\"sha256\">%s</open-checksum>\n    <location href=\"%s\"/>\n    <timestamp>%d</timestamp>\n    <size>%d</size>\n    <open-size>%d</open-size>\n  </data>\n",
			item.typ, hex.EncodeToString(gsum[:]), hex.EncodeToString(osum[:]), name, now, gz.Len(), len(open))
	}
	repomd.WriteString("</repomd>\n")
	out["repodata/repomd.xml"] = []byte(repomd.String())
	if h.a.SigningKey != "" {
		if s, err := pkgsign.LoadSigner(h.a.SigningKey, h.a.Passphrase); err == nil {
			if sig, err := s.DetachSign([]byte(repomd.String())); err == nil {
				out["repodata/repomd.xml.asc"] = sig
			}
			pub, _ := s.PublicKey()
			out["repodata/repomd.xml.key"] = []byte(pub)
		}
	}
	return out, nil
}

// rpmVerLess compares "version-release" strings with rpmvercmp semantics.
func rpmVerLess(a, b string) bool { return rpmvercmp(a, b) < 0 }

func rpmvercmp(a, b string) int {
	isAlnum := func(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		for i < len(a) && !isAlnum(a[i]) && a[i] != '~' && a[i] != '^' {
			i++
		}
		for j < len(b) && !isAlnum(b[j]) && b[j] != '~' && b[j] != '^' {
			j++
		}
		if i < len(a) && a[i] == '~' || j < len(b) && b[j] == '~' {
			if i >= len(a) || a[i] != '~' {
				return 1
			}
			if j >= len(b) || b[j] != '~' {
				return -1
			}
			i++
			j++
			continue
		}
		if i >= len(a) || j >= len(b) {
			break
		}
		si, sj := i, j
		if isDigit(a[i]) {
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			if sj == j {
				return 1
			}
			x, y := strings.TrimLeft(a[si:i], "0"), strings.TrimLeft(b[sj:j], "0")
			if len(x) != len(y) {
				if len(x) < len(y) {
					return -1
				}
				return 1
			}
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		} else {
			for i < len(a) && isAlnum(a[i]) && !isDigit(a[i]) {
				i++
			}
			for j < len(b) && isAlnum(b[j]) && !isDigit(b[j]) {
				j++
			}
			if sj == j {
				return -1
			}
			if c := strings.Compare(a[si:i], b[sj:j]); c != 0 {
				return c
			}
		}
	}
	switch {
	case i >= len(a) && j >= len(b):
		return 0
	case i >= len(a):
		return -1
	}
	return 1
}
