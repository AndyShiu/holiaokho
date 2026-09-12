package docker

import (
	"net/http"
	"net/url"
	"testing"
)

func TestParseChallenge(t *testing.T) {
	p := parseChallenge(`Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/alpine:pull,push"`)
	if p["realm"] != "https://auth.docker.io/token" || p["service"] != "registry.docker.io" || p["scope"] != "repository:library/alpine:pull,push" {
		t.Fatalf("bad parse: %v", p)
	}
}

func TestScopeFor(t *testing.T) {
	u, _ := url.Parse("https://registry-1.docker.io/v2/library/alpine/manifests/3.20")
	if s := scopeFor(&http.Request{URL: u}); s != "repository:library/alpine:pull" {
		t.Fatalf("scope %q", s)
	}
	u, _ = url.Parse("https://ghcr.io/v2/org/sub/img/blobs/sha256:abc")
	if s := scopeFor(&http.Request{URL: u}); s != "repository:org/sub/img:pull" {
		t.Fatalf("scope %q", s)
	}
}

func TestNames(t *testing.T) {
	for _, ok := range []string{"alpine", "library/alpine", "a-b.c_d/e", "org/sub/img"} {
		if !nameRe.MatchString(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"Alpine", "a//b", "/a", "a/", "a b"} {
		if nameRe.MatchString(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if !isDigest("sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e") || isDigest("latest") {
		t.Fatal("isDigest wrong")
	}
	p := (&Format{}).Parse("library/alpine/manifests/3.20")
	if p == nil || p.Namespace != "library" || p.Name != "alpine" || p.Version != "3.20" {
		t.Fatalf("Parse: %+v", p)
	}
	if (&Format{}).Parse("library/alpine/manifests/sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e") != nil {
		t.Fatal("digest refs are not packages")
	}
}
