// Package cran implements R package repositories (CRAN layout): proxy
// mirrors and hosted repositories that accept source tarballs and
// generate src/contrib/PACKAGES(.gz) from each package's DESCRIPTION.
package cran

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/common"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/storage"
)

const Name = "r"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

func isPackage(p string) bool {
	return strings.HasSuffix(p, ".tar.gz") || strings.HasSuffix(p, ".tgz") || strings.HasSuffix(p, ".zip")
}

func (Format) Parse(p string) *model.Package {
	if !isPackage(p) {
		return nil
	}
	base := path.Base(p)
	for _, ext := range []string{".tar.gz", ".tgz", ".zip"} {
		base = strings.TrimSuffix(base, ext)
	}
	name, ver, ok := strings.Cut(base, "_")
	if !ok {
		return nil
	}
	return &model.Package{Name: name, Version: ver, Namespace: path.Dir(p)}
}

type handler struct {
	repo *model.Repository
	d    format.Deps
	m    *common.Mirror
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	h := &handler{repo: r, d: d}
	h.m = &common.Mirror{Repo: r, Deps: d, Challenge: `Basic realm="Holiaokho CRAN"`,
		IsMetadata:  func(p string) bool { return !isPackage(p) },
		ContentType: common.ContentTypeByExt,
		Parse:       Format{}.Parse,
		Generate:    h.generate,
		OnDelete:    func(ctx context.Context, p string) { h.rebuild(ctx, path.Dir(p)) },
	}
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	if h.repo.Type == model.Hosted && (r.Method == http.MethodPut || r.Method == http.MethodPost) && isPackage(p) {
		if !format.Authorize(w, r, h.repo, auth.Write, h.m.Challenge) {
			return
		}
		h.upload(w, r, p)
		return
	}
	h.m.Serve(w, r)
}

// readDescription extracts and parses <pkg>/DESCRIPTION from a source tarball.
func readDescription(data []byte) (map[string]string, []string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return nil, nil, errors.New("DESCRIPTION not found")
		}
		parts := strings.Split(strings.TrimPrefix(hdr.Name, "./"), "/")
		if len(parts) == 2 && parts[1] == "DESCRIPTION" {
			raw, err := io.ReadAll(io.LimitReader(tr, 1<<20))
			if err != nil {
				return nil, nil, err
			}
			return parseDCF(string(raw))
		}
	}
}

func parseDCF(s string) (map[string]string, []string, error) {
	fields := map[string]string{}
	var keys []string
	last := ""
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if last != "" {
				fields[last] += " " + strings.TrimSpace(line)
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if _, dup := fields[k]; !dup {
			keys = append(keys, k)
		}
		fields[k] = strings.TrimSpace(v)
		last = k
	}
	if fields["Package"] == "" || fields["Version"] == "" {
		return nil, nil, errors.New("DESCRIPTION lacks Package/Version")
	}
	return fields, keys, nil
}

func (h *handler) upload(w http.ResponseWriter, r *http.Request, p string) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<30))
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "body.invalid", "read body")
		return
	}
	desc, _, err := readDescription(data)
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "cran.invalid", "%s", err.Error())
		return
	}
	if !strings.Contains(p, "/") {
		p = "src/contrib/" + desc["Package"] + "_" + desc["Version"] + ".tar.gz"
	}
	cp, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	sum := md5.Sum(data)
	attrs := map[string]any{"description": desc, "md5": hex.EncodeToString(sum[:])}
	pa, _ := json.Marshal(attrs)
	pkg := &model.Package{Name: desc["Package"], Version: desc["Version"], Namespace: path.Dir(cp), Attrs: pa}
	if _, err := h.d.Engine.Put(r.Context(), h.repo, cp, bytes.NewReader(data), repo.PutOptions{ContentType: "application/gzip", Package: pkg, Attrs: attrs}); err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	h.rebuild(r.Context(), path.Dir(cp))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"path": cp, "package": desc["Package"], "version": desc["Version"]})
}

func (h *handler) generate(ctx context.Context, rp *model.Repository, p string) ([]byte, bool) {
	if rp.Type != model.Hosted {
		return nil, false
	}
	base := path.Base(p)
	if base != "PACKAGES" && base != "PACKAGES.gz" {
		return nil, false
	}
	plain, err := h.build(ctx, rp, path.Dir(p))
	if err != nil {
		return nil, false
	}
	if base == "PACKAGES.gz" {
		var gz bytes.Buffer
		zw := gzip.NewWriter(&gz)
		zw.Write(plain)
		zw.Close()
		return gz.Bytes(), true
	}
	return plain, true
}

func (h *handler) rebuild(ctx context.Context, dir string) {
	if h.repo.Type != model.Hosted {
		return
	}
	plain, err := h.build(ctx, h.repo, dir)
	if err != nil {
		return
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(plain)
	zw.Close()
	h.d.Engine.Put(ctx, h.repo, path.Join(dir, "PACKAGES"), bytes.NewReader(plain), repo.PutOptions{ContentType: "text/plain", AllowRedeploy: true})
	h.d.Engine.Put(ctx, h.repo, path.Join(dir, "PACKAGES.gz"), bytes.NewReader(gz.Bytes()), repo.PutOptions{ContentType: "application/gzip", AllowRedeploy: true})
	// R tries PACKAGES.rds first; removing a stale one makes it fall back.
	h.d.Engine.Delete(ctx, h.repo, path.Join(dir, "PACKAGES.rds"))
}

var packagesFields = []string{"Package", "Version", "Priority", "Depends", "Imports", "LinkingTo", "Suggests", "Enhances", "License", "License_is_FOSS", "License_restricts_use", "OS_type", "Archs", "MD5sum", "NeedsCompilation"}

func (h *handler) build(ctx context.Context, rp *model.Repository, dir string) ([]byte, error) {
	assets, err := h.d.Content.ListAssets(ctx, rp.ID, dir+"/", 100000)
	if err != nil {
		return nil, err
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })
	var b bytes.Buffer
	for _, a := range assets {
		if !isPackage(a.Path) || a.BlobDigest == nil || path.Dir(a.Path) != dir {
			continue
		}
		var attrs struct {
			Description map[string]string `json:"description"`
			MD5         string            `json:"md5"`
		}
		json.Unmarshal(a.Attrs, &attrs)
		if attrs.Description == nil {
			rc, _, err := h.d.Content.OpenBlob(ctx, storage.Digest(*a.BlobDigest))
			if err != nil {
				continue
			}
			raw, _ := io.ReadAll(rc)
			rc.Close()
			desc, _, err := readDescription(raw)
			if err != nil {
				continue
			}
			sum := md5.Sum(raw)
			attrs.Description, attrs.MD5 = desc, hex.EncodeToString(sum[:])
		}
		attrs.Description["MD5sum"] = attrs.MD5
		for _, k := range packagesFields {
			if v := attrs.Description[k]; v != "" {
				b.WriteString(k + ": " + v + "\n")
			}
		}
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}
