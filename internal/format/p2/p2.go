// Package p2 implements Eclipse p2 update sites as plain mirrors:
// content/artifacts metadata (jar/xml/xz) with short TTL, plugins and
// features immutable. Hosted sites accept file uploads at any path.
package p2

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/common"
	"github.com/holiaokho/holiaokho/internal/model"
)

const Name = "p2"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

// Parse maps plugins/<id>_<version>.jar to a package.
func (Format) Parse(p string) *model.Package {
	dir := path.Dir(p)
	if (path.Base(dir) != "plugins" && path.Base(dir) != "features") || !strings.HasSuffix(p, ".jar") {
		return nil
	}
	base := strings.TrimSuffix(path.Base(p), ".jar")
	i := strings.LastIndexByte(base, '_')
	if i <= 0 {
		return nil
	}
	return &model.Package{Namespace: path.Base(dir), Name: base[:i], Version: base[i+1:]}
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	m := &common.Mirror{Repo: r, Deps: d, Challenge: `Basic realm="Holiaokho p2"`,
		IsMetadata: func(p string) bool {
			b := path.Base(p)
			return strings.HasPrefix(b, "content.") || strings.HasPrefix(b, "artifacts.") || strings.HasPrefix(b, "compositeContent.") || strings.HasPrefix(b, "compositeArtifacts.") || b == "p2.index" || b == "site.xml"
		},
		ContentType: func(p string) string {
			if strings.HasSuffix(p, ".jar") {
				return "application/java-archive"
			}
			return common.ContentTypeByExt(p)
		},
		Parse: Format{}.Parse,
	}
	return http.HandlerFunc(m.Serve)
}
