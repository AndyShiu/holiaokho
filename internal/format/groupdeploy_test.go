package format_test

import (
	"net/http/httptest"
	"testing"

	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/gitlfs"
	"github.com/holiaokho/holiaokho/internal/format/maven"
	"github.com/holiaokho/holiaokho/internal/format/npm"
)

// Which requests addressed to a group are deployments handed to its first
// hosted member, and which the group answers itself.
func TestIsGroupDeploy(t *testing.T) {
	cases := []struct {
		name   string
		f      format.Format
		method string
		path   string
		want   bool
	}{
		{"maven upload", maven.Format{}, "PUT", "/org/x/lib/1.0/lib-1.0.jar", true},
		{"maven download", maven.Format{}, "GET", "/org/x/lib/1.0/lib-1.0.jar", false},
		{"deletes are not deployments", maven.Format{}, "DELETE", "/org/x/lib/1.0/lib-1.0.jar", false},
		{"npm publish", npm.Format{}, "PUT", "/@scope%2fpkg", true},
		{"npm dist-tag", npm.Format{}, "PUT", "/-/package/pkg/dist-tags/beta", true},
		{"npm login", npm.Format{}, "PUT", "/-/user/org.couchdb.user:alice", false},
		{"npm audit", npm.Format{}, "POST", "/-/npm/v1/security/advisories/bulk", false},
		{"lfs object upload", gitlfs.Format{}, "PUT", "/info/lfs/objects/" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", true},
		{"lfs batch", gitlfs.Format{}, "POST", "/info/lfs/objects/batch", false},
		{"lfs locks", gitlfs.Format{}, "POST", "/locks", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		if got := format.IsGroupDeploy(c.f, r); got != c.want {
			t.Errorf("%s: IsGroupDeploy(%s %s) = %v, want %v", c.name, c.method, c.path, got, c.want)
		}
	}
}
