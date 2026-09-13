// Package conda implements conda channels: proxy mirrors (repodata.json
// short TTL, packages immutable) and hosted channels that accept
// .tar.bz2 / .conda uploads and generate repodata.json per subdir.
package conda

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/common"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/storage"
)

const Name = "conda"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

func isPackage(p string) bool {
	return strings.HasSuffix(p, ".tar.bz2") || strings.HasSuffix(p, ".conda")
}

// Parse maps "<subdir>/<name>-<version>-<build>.tar.bz2" to a package.
func (Format) Parse(p string) *model.Package {
	if !isPackage(p) {
		return nil
	}
	base := strings.TrimSuffix(strings.TrimSuffix(path.Base(p), ".tar.bz2"), ".conda")
	parts := strings.Split(base, "-")
	if len(parts) < 3 {
		return nil
	}
	name := strings.Join(parts[:len(parts)-2], "-")
	return &model.Package{Namespace: path.Base(path.Dir(p)), Name: name, Version: parts[len(parts)-2] + "-" + parts[len(parts)-1]}
}

type handler struct {
	repo *model.Repository
	d    format.Deps
	m    *common.Mirror
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	h := &handler{repo: r, d: d}
	h.m = &common.Mirror{Repo: r, Deps: d, Challenge: `Basic realm="Holiaokho Conda"`,
		IsMetadata: func(p string) bool { return !isPackage(p) },
		ContentType: func(p string) string {
			if strings.HasSuffix(p, ".json") {
				return "application/json"
			}
			return "application/octet-stream"
		},
		Parse:    Format{}.Parse,
		Generate: h.generate,
		OnDelete: func(ctx context.Context, p string) { h.rebuild(ctx, path.Dir(p)) },
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

// readIndex extracts info/index.json from a conda package.
func readIndex(name string, data []byte) (map[string]any, error) {
	var tr *tar.Reader
	switch {
	case strings.HasSuffix(name, ".tar.bz2"):
		tr = tar.NewReader(bzip2.NewReader(bytes.NewReader(data)))
	case strings.HasSuffix(name, ".conda"):
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if strings.HasPrefix(f.Name, "info-") && strings.HasSuffix(f.Name, ".tar.zst") {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				raw, err := io.ReadAll(io.LimitReader(rc, 64<<20))
				rc.Close()
				if err != nil {
					return nil, err
				}
				zd, err := zstd.NewReader(bytes.NewReader(raw))
				if err != nil {
					return nil, err
				}
				tr = tar.NewReader(zd)
			}
		}
		if tr == nil {
			return nil, errors.New("info tarball not found in .conda")
		}
	default:
		return nil, errors.New("unsupported package type")
	}
	for {
		hdr, err := tr.Next()
		if err != nil {
			return nil, errors.New("info/index.json not found")
		}
		if strings.TrimPrefix(hdr.Name, "./") == "info/index.json" {
			var m map[string]any
			if err := json.NewDecoder(io.LimitReader(tr, 4<<20)).Decode(&m); err != nil {
				return nil, err
			}
			return m, nil
		}
	}
}

func (h *handler) upload(w http.ResponseWriter, r *http.Request, p string) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 2<<30))
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "body.invalid", "read body")
		return
	}
	idx, err := readIndex(p, data)
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "conda.invalid", "%s", err.Error())
		return
	}
	subdir, _ := idx["subdir"].(string)
	if subdir == "" {
		subdir = "noarch"
	}
	if !strings.Contains(p, "/") {
		p = subdir + "/" + p
	}
	cp, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	sum := md5.Sum(data)
	idx["md5"] = hex.EncodeToString(sum[:])
	idx["size"] = len(data)
	pkg := Format{}.Parse(cp)
	if pkg != nil {
		pkg.Attrs, _ = json.Marshal(idx)
	}
	if _, err := h.d.Engine.Put(r.Context(), h.repo, cp, bytes.NewReader(data), repo.PutOptions{ContentType: "application/octet-stream", Package: pkg, Attrs: map[string]any{"index": idx}}); err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	h.rebuild(r.Context(), path.Dir(cp))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"path": cp, "name": idx["name"], "version": idx["version"], "subdir": subdir})
}

func (h *handler) generate(ctx context.Context, rp *model.Repository, p string) ([]byte, bool) {
	if rp.Type != model.Hosted {
		return nil, false
	}
	base := path.Base(p)
	if base != "repodata.json" && base != "current_repodata.json" {
		return nil, false
	}
	b, err := h.build(ctx, rp, path.Dir(p))
	if err != nil {
		return nil, false
	}
	return b, true
}

func (h *handler) rebuild(ctx context.Context, subdir string) {
	if h.repo.Type != model.Hosted {
		return
	}
	b, err := h.build(ctx, h.repo, subdir)
	if err != nil {
		return
	}
	for _, n := range []string{"repodata.json", "current_repodata.json"} {
		h.d.Engine.Put(ctx, h.repo, path.Join(subdir, n), bytes.NewReader(b), repo.PutOptions{ContentType: "application/json", AllowRedeploy: true})
	}
}

func (h *handler) build(ctx context.Context, rp *model.Repository, subdir string) ([]byte, error) {
	assets, err := h.d.Content.ListAssets(ctx, rp.ID, subdir+"/", 100000)
	if err != nil {
		return nil, err
	}
	packages := map[string]any{}
	packagesConda := map[string]any{}
	for _, a := range assets {
		if !isPackage(a.Path) || a.BlobDigest == nil || path.Dir(a.Path) != subdir {
			continue
		}
		var attrs struct {
			Index map[string]any `json:"index"`
		}
		json.Unmarshal(a.Attrs, &attrs)
		if attrs.Index == nil {
			rc, _, err := h.d.Content.OpenBlob(ctx, storage.Digest(*a.BlobDigest))
			if err != nil {
				continue
			}
			raw, _ := io.ReadAll(rc)
			rc.Close()
			idx, err := readIndex(a.Path, raw)
			if err != nil {
				continue
			}
			sum := md5.Sum(raw)
			idx["md5"] = hex.EncodeToString(sum[:])
			attrs.Index = idx
		}
		attrs.Index["sha256"] = strings.TrimPrefix(*a.BlobDigest, "sha256:")
		attrs.Index["size"] = a.Size
		if strings.HasSuffix(a.Path, ".conda") {
			packagesConda[path.Base(a.Path)] = attrs.Index
		} else {
			packages[path.Base(a.Path)] = attrs.Index
		}
	}
	doc := map[string]any{"info": map[string]any{"subdir": subdir}, "packages": packages, "packages.conda": packagesConda, "removed": []string{}, "repodata_version": 1}
	return json.MarshalIndent(doc, "", "  ")
}
