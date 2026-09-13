// Package cocoapods implements CocoaPods spec repositories as proxies of
// the CDN (cdn.cocoapods.org): all_pods_versions_*.txt and podspec.json
// files are mutable metadata, everything else is passed through. Pod
// sources referenced by podspecs are fetched by the client directly.
package cocoapods

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/common"
	"github.com/holiaokho/holiaokho/internal/model"
)

const Name = "cocoapods"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

// Parse maps Specs/<a>/<b>/<c>/<Pod>/<version>/<Pod>.podspec.json to a package.
func (Format) Parse(p string) *model.Package {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) == 7 && segs[0] == "Specs" && strings.HasSuffix(segs[6], ".podspec.json") {
		return &model.Package{Name: segs[4], Version: segs[5]}
	}
	return nil
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	m := &common.Mirror{Repo: r, Deps: d, Challenge: `Basic realm="Holiaokho CocoaPods"`,
		IsMetadata: func(p string) bool {
			return strings.HasPrefix(p, "all_pods") || strings.HasSuffix(p, "CocoaPods-version.yml") || strings.HasSuffix(p, "deprecated_podspecs.txt")
		},
		ContentType: common.ContentTypeByExt,
		Parse:       Format{}.Parse,
	}
	return http.HandlerFunc(m.Serve)
}
