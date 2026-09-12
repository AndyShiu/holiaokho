// Package apt implements Debian/Ubuntu APT repositories: proxy mirrors of
// upstream archives (dists/ metadata with short TTL, pool/ immutable) and
// hosted repositories that accept .deb uploads and publish signed
// Packages / Release / InRelease files.
package apt

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
)

const Name = "apt"

// Attrs is the "apt" attribute block.
type Attrs struct {
	Distribution string `json:"distribution"` // hosted: e.g. "stable"
	Component    string `json:"component"`    // hosted: e.g. "main"
	SigningKey   string `json:"signingKey,omitempty"`
	Passphrase   string `json:"passphrase,omitempty"`
	// Flat, for proxies of flat repositories (no dists/), is informational.
	Flat bool `json:"flat"`
}

func attrsOf(r *model.Repository) Attrs {
	a := Attrs{Distribution: "stable", Component: "main"}
	format.AttrBlock(r, "apt", &a)
	if a.Distribution == "" {
		a.Distribution = "stable"
	}
	if a.Component == "" {
		a.Component = "main"
	}
	return a
}

type Format struct{}

func (Format) Name() string { return Name }

func (Format) ValidateAttributes(r *model.Repository, attrs map[string]json.RawMessage) error {
	a := Attrs{}
	if raw, ok := attrs["apt"]; ok {
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("apt attributes: %w", err)
		}
	}
	if r.Type == model.Group {
		return errors.New("apt repositories cannot be grouped (use a proxy or hosted)")
	}
	if a.Distribution == "" {
		a.Distribution = "stable"
	}
	if a.Component == "" {
		a.Component = "main"
	}
	if a.SigningKey != "" {
		if _, err := pkgsign.LoadSigner(a.SigningKey, a.Passphrase); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(a)
	attrs["apt"] = raw
	return nil
}

func (Format) VersionLess(a, b string) bool { return debVersionLess(a, b) }

func (Format) Parse(p string) *model.Package {
	if !strings.HasSuffix(p, ".deb") {
		return nil
	}
	base := strings.TrimSuffix(path.Base(p), ".deb")
	parts := strings.SplitN(base, "_", 3)
	if len(parts) < 2 {
		return nil
	}
	arch := ""
	if len(parts) == 3 {
		arch = parts[2]
	}
	attrs, _ := json.Marshal(map[string]any{"arch": arch})
	return &model.Package{Name: parts[0], Version: parts[1], Namespace: arch, Attrs: attrs}
}

type handler struct {
	repo *model.Repository
	d    format.Deps
	a    Attrs
	m    *common.Mirror
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	h := &handler{repo: r, d: d, a: attrsOf(r)}
	h.m = &common.Mirror{Repo: r, Deps: d, Challenge: `Basic realm="Holiaokho APT"`,
		IsMetadata: func(p string) bool {
			return strings.HasPrefix(p, "dists/") || !strings.HasSuffix(p, ".deb") && !strings.HasPrefix(p, "pool/")
		},
		ContentType: func(p string) string {
			if strings.HasSuffix(p, ".deb") {
				return "application/vnd.debian.binary-package"
			}
			return common.ContentTypeByExt(p)
		},
		Parse:    Format{}.Parse,
		Generate: h.generate,
		OnPut:    func(ctx context.Context, p string) { h.rebuild(ctx) },
		OnDelete: func(ctx context.Context, p string) { h.rebuild(ctx) },
	}
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	// Convenience upload: POST / (or PUT <name>.deb at root) places the
	// package under pool/<component>/<initial>/<package>/.
	if h.repo.Type == model.Hosted && (r.Method == http.MethodPost || r.Method == http.MethodPut) && (p == "" || (!strings.Contains(p, "/") && strings.HasSuffix(p, ".deb"))) {
		if !format.Authorize(w, r, h.repo, auth.Write, h.m.Challenge) {
			return
		}
		h.upload(w, r)
		return
	}
	if p == "repository-key.gpg" || p == "key.gpg" || p == "public.key" {
		h.publicKey(w, r)
		return
	}
	h.m.Serve(w, r)
}

func (h *handler) publicKey(w http.ResponseWriter, r *http.Request) {
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
}

// upload reads a .deb, derives its pool path from the control file and stores it.
func (h *handler) upload(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<30))
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "body.invalid", "read body")
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = io.NopCloser(bytes.NewReader(data))
		if err := r.ParseMultipartForm(1 << 30); err == nil {
			for _, fhs := range r.MultipartForm.File {
				if len(fhs) > 0 {
					f, _ := fhs[0].Open()
					data, _ = io.ReadAll(f)
					f.Close()
					break
				}
			}
		}
	}
	c, err := ReadDeb(bytes.NewReader(data))
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "apt.invalid", "not a .deb: %s", err.Error())
		return
	}
	if c.Package == "" || c.Version == "" {
		format.WriteError(w, http.StatusBadRequest, "apt.invalid", "control file lacks Package/Version")
		return
	}
	initial := c.Package[:1]
	if strings.HasPrefix(c.Package, "lib") && len(c.Package) > 3 {
		initial = c.Package[:4]
	}
	p := fmt.Sprintf("pool/%s/%s/%s/%s_%s_%s.deb", h.a.Component, initial, c.Package, c.Package, strings.ReplaceAll(c.Version, ":", "%3a"), c.Arch)
	sum := md5.Sum(data)
	attrs := map[string]any{"control": c.Fields, "controlKeys": c.Keys, "md5": hex.EncodeToString(sum[:]), "arch": c.Arch}
	pkgAttrs, _ := json.Marshal(attrs)
	pkg := &model.Package{Name: c.Package, Version: c.Version, Namespace: c.Arch, Attrs: pkgAttrs}
	if _, err := h.d.Engine.Put(r.Context(), h.repo, p, bytes.NewReader(data), repo.PutOptions{ContentType: "application/vnd.debian.binary-package", Package: pkg, Attrs: attrs}); err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	h.rebuild(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"path": p, "package": c.Package, "version": c.Version, "architecture": c.Arch})
}

// ------------------------------------------------------------ index files

// generate builds dists/ files for hosted repositories on demand.
func (h *handler) generate(ctx context.Context, rp *model.Repository, p string) ([]byte, bool) {
	if rp.Type != model.Hosted || !strings.HasPrefix(p, "dists/"+h.a.Distribution+"/") {
		return nil, false
	}
	files, err := h.buildIndex(ctx, rp)
	if err != nil {
		h.d.Log.Warn("apt index", "err", err)
		return nil, false
	}
	b, ok := files[p]
	return b, ok
}

// rebuild materialises the index files so they are served quickly and are
// consistent with each other (Release hashes match Packages).
func (h *handler) rebuild(ctx context.Context) {
	if h.repo.Type != model.Hosted {
		return
	}
	files, err := h.buildIndex(ctx, h.repo)
	if err != nil {
		h.d.Log.Warn("apt rebuild", "repo", h.repo.Name, "err", err)
		return
	}
	for p, b := range files {
		h.d.Engine.Put(ctx, h.repo, p, bytes.NewReader(b), repo.PutOptions{ContentType: common.ContentTypeByExt(p), AllowRedeploy: true})
	}
}

func (h *handler) buildIndex(ctx context.Context, rp *model.Repository) (map[string][]byte, error) {
	assets, err := h.d.Content.ListAssets(ctx, rp.ID, "pool/", 100000)
	if err != nil {
		return nil, err
	}
	// Packages per architecture.
	byArch := map[string]*bytes.Buffer{}
	for _, a := range assets {
		if !strings.HasSuffix(a.Path, ".deb") || a.BlobDigest == nil {
			continue
		}
		var meta struct {
			Control map[string]string `json:"control"`
			Keys    []string          `json:"controlKeys"`
			MD5     string            `json:"md5"`
		}
		json.Unmarshal(a.Attrs, &meta)
		if meta.Control == nil {
			// Older upload without cached control: parse the blob now.
			rc, _, err := h.d.Content.OpenBlob(ctx, digest(*a.BlobDigest))
			if err != nil {
				continue
			}
			raw, _ := io.ReadAll(rc)
			rc.Close()
			c, err := ReadDeb(bytes.NewReader(raw))
			if err != nil {
				continue
			}
			sum := md5.Sum(raw)
			meta.Control, meta.Keys, meta.MD5 = c.Fields, c.Keys, hex.EncodeToString(sum[:])
			at := map[string]any{"control": c.Fields, "controlKeys": c.Keys, "md5": meta.MD5}
			a.Attrs, _ = json.Marshal(at)
			h.d.Content.UpsertAsset(ctx, a)
		}
		c := &Control{Fields: meta.Control, Keys: meta.Keys}
		arch := meta.Control["Architecture"]
		if arch == "" {
			arch = "all"
		}
		extra := map[string]string{"Filename": a.Path, "Size": fmt.Sprint(a.Size), "SHA256": strings.TrimPrefix(*a.BlobDigest, "sha256:"), "MD5sum": meta.MD5}
		buf := byArch[arch]
		if buf == nil {
			buf = &bytes.Buffer{}
			byArch[arch] = buf
		}
		buf.Write(c.Stanza(extra, []string{"Filename", "Size", "MD5sum", "SHA256"}))
	}
	if all, ok := byArch["all"]; ok && len(byArch) > 1 {
		// "all" packages are listed in every architecture's Packages file.
		for arch, buf := range byArch {
			if arch != "all" {
				buf.Write(all.Bytes())
			}
		}
	}
	dist := "dists/" + h.a.Distribution
	out := map[string][]byte{}
	var archs []string
	type entry struct {
		rel  string
		data []byte
	}
	var entries []entry
	for arch, buf := range byArch {
		archs = append(archs, arch)
		rel := h.a.Component + "/binary-" + arch + "/Packages"
		plain := buf.Bytes()
		var gz bytes.Buffer
		zw := gzip.NewWriter(&gz)
		zw.Write(plain)
		zw.Close()
		entries = append(entries, entry{rel, plain}, entry{rel + ".gz", gz.Bytes()})
		out[dist+"/"+rel] = plain
		out[dist+"/"+rel+".gz"] = gz.Bytes()
		// Per-component Release for apt-get's sanity checks.
		cr := fmt.Sprintf("Archive: %s\nComponent: %s\nArchitecture: %s\n", h.a.Distribution, h.a.Component, arch)
		entries = append(entries, entry{h.a.Component + "/binary-" + arch + "/Release", []byte(cr)})
		out[dist+"/"+h.a.Component+"/binary-"+arch+"/Release"] = []byte(cr)
	}
	sort.Strings(archs)
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	var rel bytes.Buffer
	fmt.Fprintf(&rel, "Origin: Holiaokho\nLabel: %s\nSuite: %s\nCodename: %s\nDate: %s\nArchitectures: %s\nComponents: %s\nDescription: Holiaokho hosted APT repository %s\n",
		rp.Name, h.a.Distribution, h.a.Distribution, time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 UTC"), strings.Join(archs, " "), h.a.Component, rp.Name)
	for _, hashName := range []string{"MD5Sum", "SHA256"} {
		rel.WriteString(hashName + ":\n")
		for _, e := range entries {
			var sum string
			if hashName == "MD5Sum" {
				s := md5.Sum(e.data)
				sum = hex.EncodeToString(s[:])
			} else {
				s := sha256.Sum256(e.data)
				sum = hex.EncodeToString(s[:])
			}
			fmt.Fprintf(&rel, " %s %16d %s\n", sum, len(e.data), e.rel)
		}
	}
	out[dist+"/Release"] = rel.Bytes()
	if h.a.SigningKey != "" {
		s, err := pkgsign.LoadSigner(h.a.SigningKey, h.a.Passphrase)
		if err != nil {
			return nil, err
		}
		if sig, err := s.DetachSign(rel.Bytes()); err == nil {
			out[dist+"/Release.gpg"] = sig
		}
		if inrel, err := s.ClearSign(rel.Bytes()); err == nil {
			out[dist+"/InRelease"] = inrel
		}
	}
	return out, nil
}

func digest(s string) storageDigest { return storageDigest(s) }
