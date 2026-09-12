package maven

import (
	"strings"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0.0", 0}, {"1", "1.0", 0}, {"1.0", "1.1", -1}, {"1.10", "1.9", 1},
		{"1.0-alpha", "1.0", -1}, {"1.0-beta", "1.0-alpha", 1}, {"1.0-rc1", "1.0-beta2", 1},
		{"1.0-SNAPSHOT", "1.0", -1}, {"1.0-SNAPSHOT", "1.0-rc1", 1}, {"1.0", "1.0-sp1", -1},
		{"2.0.0", "10.0.0", -1}, {"1.0.0-M1", "1.0.0-RC1", -1}, {"3.17.0", "3.9.0", 1},
		{"1.0-alpha-1", "1.0-alpha-2", -1}, {"1.0.0-foo", "1.0.0-bar", 1},
	}
	for _, c := range cases {
		got := Compare(c.a, c.b)
		if (got < 0 && c.want >= 0) || (got > 0 && c.want <= 0) || (got == 0 && c.want != 0) {
			t.Errorf("Compare(%q,%q) = %d, want sign %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParsePath(t *testing.T) {
	c := ParsePath("org/apache/commons/commons-lang3/3.17.0/commons-lang3-3.17.0.jar")
	if c == nil || c.GroupID != "org.apache.commons" || c.ArtifactID != "commons-lang3" || c.Version != "3.17.0" || c.Extension != "jar" || c.Classifier != "" {
		t.Fatalf("bad parse: %+v", c)
	}
	c = ParsePath("org/x/lib/1.0/lib-1.0-sources.jar.sha1")
	if c == nil || c.Classifier != "sources" || c.Checksum != "sha1" || c.Extension != "jar" {
		t.Fatalf("bad classifier/checksum parse: %+v", c)
	}
	c = ParsePath("tw/demo/lib/1.1.0-SNAPSHOT/lib-1.1.0-20260912.221618-2.jar")
	if c == nil || !c.Snapshot || c.Timestamp != "20260912.221618" || c.BuildNumber != "2" || c.BaseVersion != "1.1.0-SNAPSHOT" || c.Version != "1.1.0-20260912.221618-2" {
		t.Fatalf("bad snapshot parse: %+v", c)
	}
	c = ParsePath("tw/demo/lib/1.1.0-SNAPSHOT/lib-1.1.0-SNAPSHOT-tests.jar")
	if c == nil || !c.Snapshot || c.Timestamp != "" || c.Classifier != "tests" {
		t.Fatalf("bad non-timestamped snapshot parse: %+v", c)
	}
	c = ParsePath("org/x/lib/1.0/lib-1.0.tar.gz")
	if c == nil || c.Extension != "tar.gz" {
		t.Fatalf("bad tar.gz: %+v", c)
	}
	if ParsePath("org/x/lib/maven-metadata.xml") != nil || ParsePath("org/x/lib/1.0/other-1.0.jar") != nil || ParsePath("a/b") != nil {
		t.Fatal("expected nil for non-artifact paths")
	}
	if !IsMetadata("org/x/lib/maven-metadata.xml.sha1") || IsMetadata("org/x/lib/1.0/lib-1.0.pom") {
		t.Fatal("IsMetadata wrong")
	}
}

func TestMergeAndBuild(t *testing.T) {
	a, _ := ParseMetadata([]byte(`<metadata><groupId>g</groupId><artifactId>a</artifactId><versioning><latest>1.0</latest><release>1.0</release><versions><version>1.0</version></versions><lastUpdated>20260101000000</lastUpdated></versioning></metadata>`))
	b, _ := ParseMetadata([]byte(`<metadata><groupId>g</groupId><artifactId>a</artifactId><versioning><latest>1.1-SNAPSHOT</latest><versions><version>0.9</version><version>1.1-SNAPSHOT</version></versions><lastUpdated>20260202000000</lastUpdated></versioning></metadata>`))
	m := Merge([]*Metadata{a, b})
	v := m.Versioning
	if strings.Join(v.VersionsList(), ",") != "0.9,1.0,1.1-SNAPSHOT" || v.Latest != "1.1-SNAPSHOT" || v.Release != "1.0" || v.LastUpdated != "20260202000000" {
		t.Fatalf("bad merge: %+v", v)
	}
	out := string(m.Marshal())
	if strings.Contains(out, "<snapshotVersions>") || strings.Contains(out, "<plugins>") {
		t.Fatalf("empty containers emitted: %s", out)
	}
	ga := BuildGA("g", "a", []string{"2.0", "1.0-SNAPSHOT", "1.0"}, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if ga.Versioning.Latest != "2.0" || ga.Versioning.Release != "2.0" || ga.Versioning.LastUpdated != "20260102030405" {
		t.Fatalf("bad BuildGA: %+v", ga.Versioning)
	}
	files := []*Coordinates{
		ParsePath("g/a/1.0-SNAPSHOT/a-1.0-20260101.000000-1.jar"),
		ParsePath("g/a/1.0-SNAPSHOT/a-1.0-20260102.000000-2.jar"),
		ParsePath("g/a/1.0-SNAPSHOT/a-1.0-20260102.000000-2.pom"),
		ParsePath("g/a/1.0-SNAPSHOT/a-1.0-20260102.000000-2.jar.sha1"),
	}
	sn := BuildSnapshot("g", "a", "1.0-SNAPSHOT", files, time.Now())
	if sn.Versioning.Snapshot.BuildNumber != 2 || sn.Versioning.Snapshot.Timestamp != "20260102.000000" || len(sn.Versioning.SnapshotVersions.SnapshotVersion) != 2 {
		t.Fatalf("bad BuildSnapshot: %+v", sn.Versioning)
	}
	if Checksum("sha1", []byte("abc")) != "a9993e364706816aba3e25717850c26c9cd0d89d" {
		t.Fatal("sha1 wrong")
	}
}
