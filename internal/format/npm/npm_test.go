package npm

import "testing"

func TestSemverLess(t *testing.T) {
	cases := []struct {
		a, b string
		less bool
	}{
		{"1.0.0", "1.0.1", true}, {"1.10.0", "1.9.0", false}, {"1.0.0-alpha", "1.0.0", true},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", true}, {"1.0.0-beta.2", "1.0.0-beta.11", true},
		{"1.0.0-rc.1", "1.0.0", true}, {"2.0.0", "10.0.0", true}, {"1.0.0+build", "1.0.0", false},
		{"v1.2.3", "1.2.4", true}, {"garbage", "1.0.0", false}, {"1.0.0", "garbage", true},
	}
	for _, c := range cases {
		if got := semverLess(c.a, c.b); got != c.less {
			t.Errorf("semverLess(%q,%q)=%v want %v", c.a, c.b, got, c.less)
		}
	}
}

func TestSplitName(t *testing.T) {
	n, rest := splitName("/@babel/core/-/core-7.0.0.tgz")
	if n != "@babel/core" || len(rest) != 2 || rest[1] != "core-7.0.0.tgz" {
		t.Fatalf("scoped split wrong: %q %v", n, rest)
	}
	n, rest = splitName("lodash")
	if n != "lodash" || len(rest) != 0 {
		t.Fatalf("plain split wrong: %q %v", n, rest)
	}
	if versionFromFile("@babel/core", "core-7.24.0.tgz") != "7.24.0" || versionFromFile("lodash", "lodash-4.17.21.tgz") != "4.17.21" {
		t.Fatal("versionFromFile wrong")
	}
	if p := (Format{}).Parse("@s/n/-/n-1.2.3.tgz"); p == nil || p.Namespace != "@s" || p.Name != "n" || p.Version != "1.2.3" {
		t.Fatalf("Parse wrong: %+v", p)
	}
}

func TestMergePackuments(t *testing.T) {
	a := map[string]any{"name": "x", "versions": map[string]any{"1.0.0": map[string]any{"v": "hosted"}}, "dist-tags": map[string]any{"latest": "1.0.0"}}
	b := map[string]any{"name": "x", "description": "up", "versions": map[string]any{"1.0.0": map[string]any{"v": "proxy"}, "2.0.0": map[string]any{}}, "dist-tags": map[string]any{"latest": "2.0.0", "next": "2.0.0"}}
	m := MergePackuments([]map[string]any{a, b})
	vs := m["versions"].(map[string]any)
	if len(vs) != 2 || vs["1.0.0"].(map[string]any)["v"] != "hosted" {
		t.Fatalf("versions merge wrong: %v", vs)
	}
	tags := m["dist-tags"].(map[string]any)
	if tags["latest"] != "1.0.0" || tags["next"] != "2.0.0" || m["description"] != "up" {
		t.Fatalf("tags merge wrong: %v", tags)
	}
	m = MergePackuments([]map[string]any{{"versions": map[string]any{"1.0.0": map[string]any{}, "1.2.0": map[string]any{}, "1.10.0-beta": map[string]any{}}}})
	if m["dist-tags"].(map[string]any)["latest"] != "1.10.0-beta" {
		t.Fatalf("latest derivation wrong: %v", m["dist-tags"])
	}
}
