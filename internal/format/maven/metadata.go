package maven

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/xml"
	"sort"
	"strings"
	"time"
)

// Metadata is the maven-metadata.xml document (both G/A and G/A/V-SNAPSHOT
// flavours share this shape).
type Metadata struct {
	XMLName      xml.Name    `xml:"metadata"`
	ModelVersion string      `xml:"modelVersion,attr,omitempty"`
	GroupID      string      `xml:"groupId"`
	ArtifactID   string      `xml:"artifactId"`
	Version      string      `xml:"version,omitempty"`
	Versioning   *Versioning `xml:"versioning,omitempty"`
	Plugins      *Plugins    `xml:"plugins,omitempty"`
}

type Plugins struct {
	Plugin []Plugin `xml:"plugin"`
}

type Versioning struct {
	Latest           string            `xml:"latest,omitempty"`
	Release          string            `xml:"release,omitempty"`
	Snapshot         *Snapshot         `xml:"snapshot,omitempty"`
	Versions         *VersionList      `xml:"versions,omitempty"`
	LastUpdated      string            `xml:"lastUpdated,omitempty"`
	SnapshotVersions *SnapshotVersions `xml:"snapshotVersions,omitempty"`
}

type VersionList struct {
	Version []string `xml:"version"`
}

type SnapshotVersions struct {
	SnapshotVersion []SnapshotVersion `xml:"snapshotVersion"`
}

// VersionsList returns the version strings (nil-safe).
func (v *Versioning) VersionsList() []string {
	if v == nil || v.Versions == nil {
		return nil
	}
	return v.Versions.Version
}

func (v *Versioning) snapshotVersions() []SnapshotVersion {
	if v == nil || v.SnapshotVersions == nil {
		return nil
	}
	return v.SnapshotVersions.SnapshotVersion
}

type Snapshot struct {
	Timestamp   string `xml:"timestamp,omitempty"`
	BuildNumber int    `xml:"buildNumber,omitempty"`
	LocalCopy   bool   `xml:"localCopy,omitempty"`
}

type SnapshotVersion struct {
	Classifier string `xml:"classifier,omitempty"`
	Extension  string `xml:"extension"`
	Value      string `xml:"value"`
	Updated    string `xml:"updated"`
}

type Plugin struct {
	Name       string `xml:"name"`
	Prefix     string `xml:"prefix"`
	ArtifactID string `xml:"artifactId"`
}

func ParseMetadata(b []byte) (*Metadata, error) {
	var m Metadata
	if err := xml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Metadata) Marshal() []byte {
	out, _ := xml.MarshalIndent(m, "", "  ")
	return append([]byte(xml.Header), out...)
}

func lastUpdated(t time.Time) string { return t.UTC().Format("20060102150405") }

// Merge combines metadata documents from group members (in member order).
// Versions are unioned, latest/release recomputed, snapshot picks the newest
// build, snapshotVersions merged by (classifier, extension) keeping the newest.
func Merge(docs []*Metadata) *Metadata {
	if len(docs) == 0 {
		return nil
	}
	out := &Metadata{GroupID: docs[0].GroupID, ArtifactID: docs[0].ArtifactID, Version: docs[0].Version, ModelVersion: docs[0].ModelVersion}
	seen := map[string]bool{}
	var vers []string
	var lu string
	var snap *Snapshot
	svs := map[string]SnapshotVersion{}
	var svOrder []string
	pluginSeen := map[string]bool{}
	for _, d := range docs {
		if d.Plugins != nil {
			for _, p := range d.Plugins.Plugin {
				k := p.Prefix + "/" + p.ArtifactID
				if !pluginSeen[k] {
					pluginSeen[k] = true
					if out.Plugins == nil {
						out.Plugins = &Plugins{}
					}
					out.Plugins.Plugin = append(out.Plugins.Plugin, p)
				}
			}
		}
		if d.Versioning == nil {
			continue
		}
		for _, v := range d.Versioning.VersionsList() {
			if !seen[v] {
				seen[v] = true
				vers = append(vers, v)
			}
		}
		if d.Versioning.LastUpdated > lu {
			lu = d.Versioning.LastUpdated
		}
		if s := d.Versioning.Snapshot; s != nil {
			if snap == nil || s.Timestamp > snap.Timestamp || (s.Timestamp == snap.Timestamp && s.BuildNumber > snap.BuildNumber) {
				cp := *s
				snap = &cp
			}
		}
		for _, sv := range d.Versioning.snapshotVersions() {
			k := sv.Classifier + "\x00" + sv.Extension
			if cur, ok := svs[k]; !ok || sv.Updated > cur.Updated {
				if !ok {
					svOrder = append(svOrder, k)
				}
				svs[k] = sv
			}
		}
	}
	if len(vers) == 0 && snap == nil && len(svs) == 0 && lu == "" {
		return out
	}
	v := &Versioning{LastUpdated: lu, Snapshot: snap}
	if len(vers) > 0 {
		sort.Slice(vers, func(i, j int) bool { return Less(vers[i], vers[j]) })
		v.Versions = &VersionList{Version: vers}
		for _, x := range vers {
			v.Latest = x
			if !IsSnapshotVersion(x) {
				v.Release = x
			}
		}
	}
	if len(svOrder) > 0 {
		list := make([]SnapshotVersion, 0, len(svOrder))
		for _, k := range svOrder {
			list = append(list, svs[k])
		}
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if a.Extension != b.Extension {
				return a.Extension < b.Extension
			}
			return a.Classifier < b.Classifier
		})
		v.SnapshotVersions = &SnapshotVersions{SnapshotVersion: list}
	}
	out.Versioning = v
	return out
}

// BuildGA generates group/artifact level metadata from a version list.
func BuildGA(groupID, artifactID string, versions []string, updated time.Time) *Metadata {
	sort.Slice(versions, func(i, j int) bool { return Less(versions[i], versions[j]) })
	v := &Versioning{Versions: &VersionList{Version: versions}, LastUpdated: lastUpdated(updated)}
	for _, x := range versions {
		v.Latest = x
		if !IsSnapshotVersion(x) {
			v.Release = x
		}
	}
	return &Metadata{ModelVersion: "1.1.0", GroupID: groupID, ArtifactID: artifactID, Versioning: v}
}

// BuildSnapshot generates version-level metadata for a -SNAPSHOT directory
// from the artifact coordinates found in it.
func BuildSnapshot(groupID, artifactID, baseVersion string, files []*Coordinates, updated time.Time) *Metadata {
	var latest *Coordinates
	byKey := map[string]SnapshotVersion{}
	var order []string
	for _, c := range files {
		if c.Checksum != "" || c.Signature || c.Timestamp == "" {
			continue
		}
		if latest == nil || c.Timestamp > latest.Timestamp || (c.Timestamp == latest.Timestamp && c.BuildNumber > latest.BuildNumber) {
			latest = c
		}
		k := c.Classifier + "\x00" + c.Extension
		sv := SnapshotVersion{Classifier: c.Classifier, Extension: c.Extension, Value: c.Version, Updated: strings.ReplaceAll(c.Timestamp, ".", "")}
		if cur, ok := byKey[k]; !ok || sv.Updated > cur.Updated {
			if !ok {
				order = append(order, k)
			}
			byKey[k] = sv
		}
	}
	m := &Metadata{ModelVersion: "1.1.0", GroupID: groupID, ArtifactID: artifactID, Version: baseVersion, Versioning: &Versioning{LastUpdated: lastUpdated(updated)}}
	if latest != nil {
		bn := 0
		for _, ch := range latest.BuildNumber {
			bn = bn*10 + int(ch-'0')
		}
		m.Versioning.Snapshot = &Snapshot{Timestamp: latest.Timestamp, BuildNumber: bn}
	}
	if len(order) > 0 {
		m.Versioning.SnapshotVersions = &SnapshotVersions{}
		for _, k := range order {
			m.Versioning.SnapshotVersions.SnapshotVersion = append(m.Versioning.SnapshotVersions.SnapshotVersion, byKey[k])
		}
	}
	return m
}

// Checksum computes the hex digest named by ext (sha1, md5, sha256, sha512).
func Checksum(ext string, b []byte) string {
	switch ext {
	case "sha1":
		s := sha1.Sum(b)
		return hex.EncodeToString(s[:])
	case "md5":
		s := md5.Sum(b)
		return hex.EncodeToString(s[:])
	case "sha256":
		s := sha256.Sum256(b)
		return hex.EncodeToString(s[:])
	case "sha512":
		s := sha512.Sum512(b)
		return hex.EncodeToString(s[:])
	}
	return ""
}
