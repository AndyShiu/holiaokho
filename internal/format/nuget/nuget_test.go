package nuget

import "testing"

func TestVersions(t *testing.T) {
	cases := map[string]string{"1.0": "1.0.0", "1.0.0.0": "1.0.0", "1.02.3": "1.2.3", "2.0.0-Beta1+meta": "2.0.0-beta1", "1.0.0.1": "1.0.0.1"}
	for in, want := range cases {
		if got := NormalizeVersion(in); got != want {
			t.Errorf("Normalize(%s)=%s want %s", in, got, want)
		}
	}
	for _, c := range [][2]string{{"1.0.0", "1.0.1"}, {"1.0.0-beta", "1.0.0"}, {"1.0.0-alpha", "1.0.0-beta"}, {"1.9.0", "1.10.0"}, {"1.0.0-beta.2", "1.0.0-beta.11"}} {
		if !VersionLess(c[0], c[1]) || VersionLess(c[1], c[0]) {
			t.Errorf("expected %s < %s", c[0], c[1])
		}
	}
}

func TestNuspec(t *testing.T) {
	ns, err := ParseNuspec([]byte(`<?xml version="1.0"?><package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd"><metadata minClientVersion="2.12"><id>My.Lib</id><version>1.2.3</version><authors>me</authors><description>d</description>
<dependencies><group targetFramework="net8.0"><dependency id="Newtonsoft.Json" version="13.0.3" /></group><group targetFramework="netstandard2.0" /></dependencies></metadata></package>`))
	if err != nil {
		t.Fatal(err)
	}
	if ns.ID != "My.Lib" || ns.Version != "1.2.3" || len(ns.DependencyGroups) != 2 || ns.DependencyGroups[0].Dependencies[0].ID != "Newtonsoft.Json" || ns.MinClientVersion != "2.12" {
		t.Fatalf("bad nuspec: %+v", ns)
	}
	if len(ns.DependencyGroups[1].Dependencies) != 0 {
		t.Fatal("empty group must have empty (non-nil) deps")
	}
}
