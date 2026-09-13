package npm

import (
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
)

// A packument shaped like a popular package: many versions, each a deep object.
func syntheticPackument(versions int) []byte {
	doc := map[string]any{"name": "big", "dist-tags": map[string]any{"latest": "1.0.0"}}
	vs := map[string]any{}
	for i := 0; i < versions; i++ {
		v := fmt.Sprintf("1.0.%d", i)
		deps := map[string]any{}
		for j := 0; j < 30; j++ {
			deps[fmt.Sprintf("dep-%d", j)] = "^1.2.3"
		}
		vs[v] = map[string]any{
			"name": "big", "version": v, "dependencies": deps,
			"devDependencies": deps, "scripts": map[string]any{"build": "tsc", "test": "jest"},
			"dist": map[string]any{"tarball": "https://registry.npmjs.org/big/-/big-" + v + ".tgz", "shasum": "abc", "integrity": "sha512-xxx"},
		}
	}
	doc["versions"] = vs
	b, _ := json.Marshal(doc)
	return b
}

// TestPackumentDecodeFootprint guards the reason versions are kept as raw
// JSON: decoding them into map[string]any costs several times the document
// size, which is what exhausted the memory limit of a real deployment.
func TestPackumentDecodeFootprint(t *testing.T) {
	raw := syntheticPackument(800)

	measure := func(fn func() any) float64 {
		runtime.GC()
		var a, b runtime.MemStats
		runtime.ReadMemStats(&a)
		v := fn()
		runtime.ReadMemStats(&b)
		runtime.KeepAlive(v)
		return float64(b.HeapAlloc-a.HeapAlloc) / float64(len(raw))
	}
	full := measure(func() any {
		var d map[string]any
		json.Unmarshal(raw, &d)
		return d
	})
	raws := measure(func() any {
		d, err := decodePackument(raw)
		if err != nil {
			t.Fatal(err)
		}
		return d
	})
	t.Logf("heap per byte of JSON: map[string]any %.1fx, decodePackument %.1fx", full, raws)
	if raws > 2 {
		t.Errorf("decodePackument should stay close to the document size, got %.1fx", raws)
	}
	if raws >= full {
		t.Errorf("decodePackument (%.1fx) should beat map[string]any (%.1fx)", raws, full)
	}
}

// Version objects must survive the round trip untouched apart from the tarball.
func TestRewriteVersionTarballKeepsFields(t *testing.T) {
	in := json.RawMessage(`{"name":"x","version":"1.0.0","dependencies":{"a":"^1"},"dist":{"tarball":"https://registry.npmjs.org/x/-/x-1.0.0.tgz","shasum":"s"}}`)
	out, ok := rewriteVersionTarball(in, "http://host/repository/npm/x/-/")
	if !ok {
		t.Fatal("rewrite failed")
	}
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	dist := v["dist"].(map[string]any)
	if dist["tarball"] != "http://host/repository/npm/x/-/x-1.0.0.tgz" {
		t.Errorf("tarball not rewritten: %v", dist["tarball"])
	}
	if dist["shasum"] != "s" {
		t.Errorf("dist fields lost: %v", dist)
	}
	if v["version"] != "1.0.0" || v["dependencies"].(map[string]any)["a"] != "^1" {
		t.Errorf("version fields lost: %v", v)
	}
}
