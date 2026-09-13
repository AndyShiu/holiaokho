// Package alpine implements Alpine Linux apk repositories: proxy mirrors
// (APKINDEX.tar.gz short TTL, .apk immutable) and hosted repositories that
// accept .apk uploads and publish an RSA-signed APKINDEX per architecture.
package alpine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
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
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/storage"
)

const Name = "alpine"

type Attrs struct {
	// SigningKey is a PEM RSA private key; KeyName is the file name clients
	// install under /etc/apk/keys (e.g. "holiaokho.rsa.pub").
	SigningKey string `json:"signingKey,omitempty"`
	KeyName    string `json:"keyName"`
}

func attrsOf(r *model.Repository) Attrs {
	a := Attrs{KeyName: "holiaokho.rsa.pub"}
	format.AttrBlock(r, "alpine", &a)
	if a.KeyName == "" {
		a.KeyName = "holiaokho.rsa.pub"
	}
	return a
}

type Format struct{}

func (Format) Name() string { return Name }

func (Format) ValidateAttributes(r *model.Repository, attrs map[string]json.RawMessage) error {
	a := Attrs{KeyName: "holiaokho.rsa.pub"}
	if raw, ok := attrs["alpine"]; ok {
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("alpine attributes: %w", err)
		}
	}
	if a.SigningKey != "" {
		if _, err := parseRSAKey(a.SigningKey); err != nil {
			return err
		}
	}
	if a.KeyName == "" {
		a.KeyName = "holiaokho.rsa.pub"
	}
	raw, _ := json.Marshal(a)
	attrs["alpine"] = raw
	return nil
}

func (Format) VersionLess(a, b string) bool { return a < b }

func (Format) Parse(p string) *model.Package {
	if !strings.HasSuffix(p, ".apk") {
		return nil
	}
	base := strings.TrimSuffix(path.Base(p), ".apk")
	// name-version-rN: version starts at the first "-<digit>".
	for i := 1; i < len(base)-1; i++ {
		if base[i] == '-' && base[i+1] >= '0' && base[i+1] <= '9' {
			return &model.Package{Name: base[:i], Version: base[i+1:], Namespace: path.Base(path.Dir(p))}
		}
	}
	return nil
}

func parseRSAKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("signing key is not PEM")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse RSA key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("signing key must be RSA")
	}
	return rk, nil
}

// GenerateKey returns a PEM RSA private key and its public key in the
// format apk expects under /etc/apk/keys.
func GenerateKey() (private, public string, err error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	priv := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	pubDER, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		return "", "", err
	}
	pub := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return string(priv), string(pub), nil
}

func publicPEM(k *rsa.PrivateKey) string {
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

type handler struct {
	repo *model.Repository
	d    format.Deps
	a    Attrs
	m    *common.Mirror
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	h := &handler{repo: r, d: d, a: attrsOf(r)}
	h.m = &common.Mirror{Repo: r, Deps: d, Challenge: `Basic realm="Holiaokho Alpine"`,
		IsMetadata: func(p string) bool { return !strings.HasSuffix(p, ".apk") },
		ContentType: func(p string) string {
			if strings.HasSuffix(p, ".apk") {
				return "application/vnd.alpine.package"
			}
			return common.ContentTypeByExt(p)
		},
		Parse:    Format{}.Parse,
		Generate: h.generate,
		OnDelete: func(ctx context.Context, p string) { h.rebuild(ctx, path.Dir(p)) },
	}
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	if h.repo.Type == model.Hosted && (r.Method == http.MethodPost || r.Method == http.MethodPut) && strings.HasSuffix(p, ".apk") {
		if !format.Authorize(w, r, h.repo, auth.Write, h.m.Challenge) {
			return
		}
		h.upload(w, r, p)
		return
	}
	if strings.HasPrefix(p, "keys/") || p == h.a.KeyName {
		if h.a.SigningKey == "" {
			format.WriteError(w, http.StatusNotFound, "not_found", "repository is not signed")
			return
		}
		k, err := parseRSAKey(h.a.SigningKey)
		if err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		format.ServeBytes(w, r, "application/x-pem-file", []byte(publicPEM(k)), time.Time{})
		return
	}
	h.m.Serve(w, r)
}

// ------------------------------------------------------------- apk parsing

// PkgInfo is the parsed .PKGINFO plus the control-segment checksum.
type PkgInfo struct {
	Fields   map[string][]string `json:"fields"`
	Checksum string              `json:"checksum"` // "Q1<base64 sha1>"
}

func (p *PkgInfo) get(k string) string {
	if v := p.Fields[k]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// ReadAPK splits the gzip members of an .apk, computes the control checksum
// and parses .PKGINFO.
func ReadAPK(data []byte) (*PkgInfo, error) {
	br := bytes.NewReader(data)
	var segments [][]byte
	var tars [][]byte
	for br.Len() > 0 && len(segments) < 3 {
		start := int64(len(data)) - int64(br.Len())
		zr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("apk gzip: %w", err)
		}
		zr.Multistream(false)
		content, err := io.ReadAll(io.LimitReader(zr, 64<<20))
		if err != nil {
			return nil, err
		}
		zr.Close()
		end := int64(len(data)) - int64(br.Len())
		segments = append(segments, data[start:end])
		tars = append(tars, content)
		if len(segments) == 2 || (len(segments) == 1 && !hasSignature(content)) {
			// Stop after the control segment.
			if len(segments) == 1 && !hasSignature(content) {
				break
			}
			break
		}
	}
	ctrlIdx := 0
	if len(tars) > 1 {
		ctrlIdx = 1
	}
	if ctrlIdx >= len(tars) {
		return nil, errors.New("apk: control segment missing")
	}
	sum := sha1.Sum(segments[ctrlIdx])
	info := &PkgInfo{Fields: map[string][]string{}, Checksum: "Q1" + base64.StdEncoding.EncodeToString(sum[:])}
	tr := tar.NewReader(bytes.NewReader(tars[ctrlIdx]))
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		if hdr.Name == ".PKGINFO" {
			raw, _ := io.ReadAll(io.LimitReader(tr, 1<<20))
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "#") || !strings.Contains(line, " = ") {
					continue
				}
				k, v, _ := strings.Cut(line, " = ")
				info.Fields[k] = append(info.Fields[k], v)
			}
			break
		}
	}
	if info.get("pkgname") == "" {
		return nil, errors.New("apk: .PKGINFO missing pkgname")
	}
	return info, nil
}

func hasSignature(tarBytes []byte) bool {
	tr := tar.NewReader(bytes.NewReader(tarBytes))
	hdr, err := tr.Next()
	return err == nil && strings.HasPrefix(hdr.Name, ".SIGN.")
}

// ---------------------------------------------------------------- upload

func (h *handler) upload(w http.ResponseWriter, r *http.Request, p string) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<30))
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "body.invalid", "read body")
		return
	}
	info, err := ReadAPK(data)
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "alpine.invalid", "%s", err.Error())
		return
	}
	arch := info.get("arch")
	if arch == "" {
		arch = "noarch"
	}
	if !strings.Contains(p, "/") {
		p = arch + "/" + p
	}
	cp, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	meta, _ := json.Marshal(info)
	pkg := &model.Package{Name: info.get("pkgname"), Version: info.get("pkgver"), Namespace: arch, Attrs: meta}
	if _, err := h.d.Engine.Put(r.Context(), h.repo, cp, bytes.NewReader(data), repo.PutOptions{ContentType: "application/vnd.alpine.package", Package: pkg, Attrs: map[string]any{"apk": json.RawMessage(meta)}}); err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	h.rebuild(r.Context(), path.Dir(cp))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"path": cp, "package": pkg.Name, "version": pkg.Version, "arch": arch})
}

// ------------------------------------------------------------------ index

func (h *handler) generate(ctx context.Context, rp *model.Repository, p string) ([]byte, bool) {
	if rp.Type != model.Hosted || path.Base(p) != "APKINDEX.tar.gz" {
		return nil, false
	}
	b, err := h.buildIndex(ctx, rp, path.Dir(p))
	if err != nil {
		return nil, false
	}
	return b, true
}

func (h *handler) rebuild(ctx context.Context, dir string) {
	if h.repo.Type != model.Hosted {
		return
	}
	b, err := h.buildIndex(ctx, h.repo, dir)
	if err != nil {
		h.d.Log.Warn("apk index", "repo", h.repo.Name, "dir", dir, "err", err)
		return
	}
	h.d.Engine.Put(ctx, h.repo, path.Join(dir, "APKINDEX.tar.gz"), bytes.NewReader(b), repo.PutOptions{ContentType: "application/gzip", AllowRedeploy: true})
}

var indexKeys = []struct{ pkginfo, idx string }{
	{"pkgname", "P"}, {"pkgver", "V"}, {"arch", "A"}, {"size", "I"}, {"pkgdesc", "T"}, {"url", "U"}, {"license", "L"},
	{"origin", "o"}, {"maintainer", "m"}, {"builddate", "t"}, {"commit", "c"}, {"provider_priority", "k"},
}

func (h *handler) buildIndex(ctx context.Context, rp *model.Repository, dir string) ([]byte, error) {
	assets, err := h.d.Content.ListAssets(ctx, rp.ID, strings.TrimSuffix(dir, "/")+"/", 100000)
	if err != nil {
		return nil, err
	}
	var idx bytes.Buffer
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })
	for _, a := range assets {
		if !strings.HasSuffix(a.Path, ".apk") || a.BlobDigest == nil || path.Dir(a.Path) != strings.TrimSuffix(dir, "/") {
			continue
		}
		var attrs struct {
			APK *PkgInfo `json:"apk"`
		}
		json.Unmarshal(a.Attrs, &attrs)
		if attrs.APK == nil {
			rc, _, err := h.d.Content.OpenBlob(ctx, storage.Digest(*a.BlobDigest))
			if err != nil {
				continue
			}
			raw, _ := io.ReadAll(rc)
			rc.Close()
			info, err := ReadAPK(raw)
			if err != nil {
				continue
			}
			attrs.APK = info
		}
		info := attrs.APK
		fmt.Fprintf(&idx, "C:%s\n", info.Checksum)
		for _, k := range indexKeys {
			if v := info.get(k.pkginfo); v != "" {
				fmt.Fprintf(&idx, "%s:%s\n", k.idx, v)
			}
		}
		fmt.Fprintf(&idx, "S:%d\n", a.Size)
		if d := info.Fields["depend"]; len(d) > 0 {
			fmt.Fprintf(&idx, "D:%s\n", strings.Join(d, " "))
		}
		if p := info.Fields["provides"]; len(p) > 0 {
			fmt.Fprintf(&idx, "p:%s\n", strings.Join(p, " "))
		}
		if i := info.Fields["install_if"]; len(i) > 0 {
			fmt.Fprintf(&idx, "i:%s\n", strings.Join(i, " "))
		}
		idx.WriteString("\n")
	}
	// Second gzip member: tar(APKINDEX, DESCRIPTION).
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	tw := tar.NewWriter(gz)
	now := time.Now()
	for _, f := range []struct {
		name string
		data []byte
	}{{"DESCRIPTION", []byte("Holiaokho " + rp.Name)}, {"APKINDEX", idx.Bytes()}} {
		tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o644, Size: int64(len(f.data)), ModTime: now, Uname: "root", Gname: "root"})
		tw.Write(f.data)
	}
	tw.Close()
	gz.Close()
	if h.a.SigningKey == "" {
		return body.Bytes(), nil
	}
	key, err := parseRSAKey(h.a.SigningKey)
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum(body.Bytes())
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA1, sum[:])
	if err != nil {
		return nil, err
	}
	// First gzip member: tar(.SIGN.RSA.<key>) with the end-of-archive
	// blocks cut off (abuild-tar --cut), then the body member.
	var sigTar bytes.Buffer
	stw := tar.NewWriter(&sigTar)
	stw.WriteHeader(&tar.Header{Name: ".SIGN.RSA." + h.a.KeyName, Mode: 0o644, Size: int64(len(sig)), ModTime: now, Uname: "root", Gname: "root"})
	stw.Write(sig)
	stw.Close()
	cut := sigTar.Bytes()
	if len(cut) >= 1024 {
		cut = cut[:len(cut)-1024]
	}
	var out bytes.Buffer
	sgz := gzip.NewWriter(&out)
	sgz.Write(cut)
	sgz.Close()
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

// Rebuild regenerates APKINDEX.tar.gz for every architecture directory.
func (f Format) Rebuild(ctx context.Context, d format.Deps, rp *model.Repository) error {
	h := &handler{repo: rp, d: d, a: attrsOf(rp)}
	dirs, _, err := d.Content.ListChildren(ctx, rp.ID, "")
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		h.rebuild(ctx, dir)
	}
	return nil
}
