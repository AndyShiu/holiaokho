// Package gitlfs implements the Git LFS batch API with the basic transfer
// adapter (hosted only): objects are content-addressed by their sha256
// oid, which maps 1:1 onto Holiaokho blob storage.
package gitlfs

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/storage"
)

const Name = "gitlfs"

type Format struct{}

func (Format) Name() string { return Name }

func (Format) ValidateAttributes(r *model.Repository, attrs map[string]json.RawMessage) error {
	if r.Type != model.Hosted {
		return fmt.Errorf("gitlfs repositories must be hosted")
	}
	return nil
}
func (Format) VersionLess(a, b string) bool  { return a < b }
func (Format) Parse(p string) *model.Package { return nil }

var oidRe = regexp.MustCompile(`^[a-f0-9]{64}$`)

type handler struct {
	repo *model.Repository
	d    format.Deps
}

// locks are kept in memory (single node); ids map to lock records.
var (
	locksMu sync.Mutex
	locks   = map[string]map[string]lock{} // repo -> id -> lock
)

type lock struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	LockedAt time.Time `json:"locked_at"`
	Owner    struct {
		Name string `json:"name"`
	} `json:"owner"`
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

const challenge = `Basic realm="Holiaokho Git LFS"`

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/vnd.git-lfs+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *handler) base(r *http.Request) string { return h.d.BaseURL(r) + "/repository/" + h.repo.Name }

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	// git-lfs may use the repository URL with or without ".git/info/lfs".
	p = strings.TrimPrefix(strings.TrimPrefix(p, "info/lfs/"), "info/lfs")
	p = strings.Trim(p, "/")
	switch {
	case p == "objects/batch" && r.Method == http.MethodPost:
		h.batch(w, r)
	case strings.HasPrefix(p, "objects/") && oidRe.MatchString(strings.TrimPrefix(p, "objects/")):
		oid := strings.TrimPrefix(p, "objects/")
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
				return
			}
			format.FetchAndServe(w, r, h.d, h.repo, "objects/"+oid, repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/octet-stream"})
		case http.MethodPut:
			if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
				return
			}
			_, err := h.d.Engine.Put(r.Context(), h.repo, "objects/"+oid, r.Body, repo.PutOptions{ContentType: "application/octet-stream", Digest: storage.DigestFromHex(oid), AllowRedeploy: true})
			if err != nil {
				if strings.Contains(err.Error(), "mismatch") {
					writeJSON(w, 422, map[string]any{"message": "object does not match oid"})
					return
				}
				format.MapError(w, err, h.d.Log)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			writeJSON(w, 405, map[string]any{"message": "method not allowed"})
		}
	case p == "verify" && r.Method == http.MethodPost:
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		var in struct {
			Oid  string `json:"oid"`
			Size int64  `json:"size"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		size, ok, _ := h.d.Content.BlobExists(r.Context(), storage.DigestFromHex(in.Oid))
		if !ok || size != in.Size {
			writeJSON(w, 422, map[string]any{"message": "object missing or size mismatch"})
			return
		}
		w.WriteHeader(http.StatusOK)
	case strings.HasPrefix(p, "locks"):
		h.locks(w, r, strings.TrimPrefix(strings.TrimPrefix(p, "locks"), "/"))
	default:
		writeJSON(w, 404, map[string]any{"message": "not found"})
	}
}

func (h *handler) batch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Operation string   `json:"operation"`
		Transfers []string `json:"transfers"`
		Objects   []struct {
			Oid  string `json:"oid"`
			Size int64  `json:"size"`
		} `json:"objects"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]any{"message": "invalid batch request"})
		return
	}
	action := auth.Read
	if in.Operation == "upload" {
		action = auth.Write
	}
	if !format.Authorize(w, r, h.repo, action, challenge) {
		return
	}
	base := h.base(r)
	hdr := map[string]string{}
	if a := r.Header.Get("Authorization"); a != "" {
		hdr["Authorization"] = a
	}
	var objects []map[string]any
	for _, o := range in.Objects {
		if !oidRe.MatchString(o.Oid) {
			objects = append(objects, map[string]any{"oid": o.Oid, "size": o.Size, "error": map[string]any{"code": 422, "message": "invalid oid"}})
			continue
		}
		size, exists, _ := h.d.Content.BlobExists(r.Context(), storage.DigestFromHex(o.Oid))
		if exists {
			// Make sure the object is visible in this repository.
			if _, err := h.d.Content.Asset(r.Context(), h.repo.ID, "objects/"+o.Oid); err != nil {
				d := "sha256:" + o.Oid
				h.d.Content.UpsertAsset(r.Context(), &model.Asset{RepoID: h.repo.ID, Path: "objects/" + o.Oid, BlobDigest: &d, Size: size, ContentType: "application/octet-stream"})
			}
		}
		obj := map[string]any{"oid": o.Oid, "size": o.Size, "authenticated": true}
		switch in.Operation {
		case "download":
			if !exists {
				obj["error"] = map[string]any{"code": 404, "message": "object not found"}
			} else {
				obj["actions"] = map[string]any{"download": map[string]any{"href": base + "/objects/" + o.Oid, "header": hdr}}
			}
		case "upload":
			if !exists {
				obj["actions"] = map[string]any{
					"upload": map[string]any{"href": base + "/objects/" + o.Oid, "header": hdr},
					"verify": map[string]any{"href": base + "/verify", "header": hdr},
				}
			}
		default:
			writeJSON(w, 400, map[string]any{"message": "operation must be download or upload"})
			return
		}
		objects = append(objects, obj)
	}
	if objects == nil {
		objects = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"transfer": "basic", "objects": objects})
}

// locks implements the File Locking API (in memory).
func (h *handler) locks(w http.ResponseWriter, r *http.Request, rest string) {
	p := auth.PrincipalFrom(r.Context())
	if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
		return
	}
	locksMu.Lock()
	defer locksMu.Unlock()
	m := locks[h.repo.Name]
	if m == nil {
		m = map[string]lock{}
		locks[h.repo.Name] = m
	}
	switch {
	case rest == "" && r.Method == http.MethodPost:
		var in struct {
			Path string `json:"path"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		for _, l := range m {
			if l.Path == in.Path {
				writeJSON(w, 409, map[string]any{"lock": l, "message": "already locked"})
				return
			}
		}
		l := lock{ID: uuid.NewString(), Path: in.Path, LockedAt: time.Now().UTC()}
		l.Owner.Name = p.Username
		m[l.ID] = l
		writeJSON(w, 201, map[string]any{"lock": l})
	case rest == "" && r.Method == http.MethodGet:
		var list []lock
		for _, l := range m {
			list = append(list, l)
		}
		if list == nil {
			list = []lock{}
		}
		writeJSON(w, 200, map[string]any{"locks": list})
	case rest == "verify" && r.Method == http.MethodPost:
		ours, theirs := []lock{}, []lock{}
		for _, l := range m {
			if l.Owner.Name == p.Username {
				ours = append(ours, l)
			} else {
				theirs = append(theirs, l)
			}
		}
		writeJSON(w, 200, map[string]any{"ours": ours, "theirs": theirs})
	case strings.HasSuffix(rest, "/unlock") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(rest, "/unlock")
		l, ok := m[id]
		if !ok {
			writeJSON(w, 404, map[string]any{"message": "lock not found"})
			return
		}
		delete(m, id)
		writeJSON(w, 200, map[string]any{"lock": l})
	default:
		writeJSON(w, 404, map[string]any{"message": "not found"})
	}
}
