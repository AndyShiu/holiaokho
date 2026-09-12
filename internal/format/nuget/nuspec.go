package nuget

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
)

// Nuspec is the subset of the .nuspec manifest needed for registration.
type Nuspec struct {
	ID               string     `json:"id"`
	Version          string     `json:"version"`
	Title            string     `json:"title,omitempty"`
	Authors          string     `json:"authors,omitempty"`
	Description      string     `json:"description,omitempty"`
	Summary          string     `json:"summary,omitempty"`
	Tags             string     `json:"tags,omitempty"`
	ProjectURL       string     `json:"projectUrl,omitempty"`
	LicenseURL       string     `json:"licenseUrl,omitempty"`
	IconURL          string     `json:"iconUrl,omitempty"`
	RequireLicense   bool       `json:"requireLicenseAcceptance"`
	MinClientVersion string     `json:"minClientVersion,omitempty"`
	DependencyGroups []DepGroup `json:"dependencyGroups"`
}

type DepGroup struct {
	TargetFramework string       `json:"targetFramework,omitempty"`
	Dependencies    []Dependency `json:"dependencies"`
}

type Dependency struct {
	ID    string `json:"id"`
	Range string `json:"range,omitempty"`
}

type xmlNuspec struct {
	Metadata struct {
		ID           string `xml:"id"`
		Version      string `xml:"version"`
		Title        string `xml:"title"`
		Authors      string `xml:"authors"`
		Description  string `xml:"description"`
		Summary      string `xml:"summary"`
		Tags         string `xml:"tags"`
		ProjectURL   string `xml:"projectUrl"`
		LicenseURL   string `xml:"licenseUrl"`
		IconURL      string `xml:"iconUrl"`
		RequireLic   string `xml:"requireLicenseAcceptance"`
		MinClient    string `xml:"minClientVersion,attr"`
		Dependencies struct {
			Dependency []xmlDep `xml:"dependency"`
			Group      []struct {
				TargetFramework string   `xml:"targetFramework,attr"`
				Dependency      []xmlDep `xml:"dependency"`
			} `xml:"group"`
		} `xml:"dependencies"`
	} `xml:"metadata"`
}

type xmlDep struct {
	ID      string `xml:"id,attr"`
	Version string `xml:"version,attr"`
}

func ParseNuspec(b []byte) (*Nuspec, error) {
	var x xmlNuspec
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) { return input, nil }
	if err := dec.Decode(&x); err != nil {
		return nil, err
	}
	m := x.Metadata
	if m.ID == "" || m.Version == "" {
		return nil, errors.New("nuspec missing id or version")
	}
	n := &Nuspec{ID: m.ID, Version: m.Version, Title: m.Title, Authors: m.Authors, Description: m.Description, Summary: m.Summary, Tags: m.Tags,
		ProjectURL: m.ProjectURL, LicenseURL: m.LicenseURL, IconURL: m.IconURL, RequireLicense: strings.EqualFold(m.RequireLic, "true"), MinClientVersion: m.MinClient}
	if len(m.Dependencies.Dependency) > 0 {
		g := DepGroup{}
		for _, d := range m.Dependencies.Dependency {
			g.Dependencies = append(g.Dependencies, Dependency{ID: d.ID, Range: d.Version})
		}
		n.DependencyGroups = append(n.DependencyGroups, g)
	}
	for _, grp := range m.Dependencies.Group {
		g := DepGroup{TargetFramework: grp.TargetFramework, Dependencies: []Dependency{}}
		for _, d := range grp.Dependency {
			g.Dependencies = append(g.Dependencies, Dependency{ID: d.ID, Range: d.Version})
		}
		n.DependencyGroups = append(n.DependencyGroups, g)
	}
	if n.DependencyGroups == nil {
		n.DependencyGroups = []DepGroup{}
	}
	return n, nil
}

// ReadNupkg extracts the .nuspec from a .nupkg (zip) held in memory.
func ReadNupkg(b []byte) (*Nuspec, []byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, nil, err
	}
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".nuspec") && !strings.Contains(f.Name, "/") {
			rc, err := f.Open()
			if err != nil {
				return nil, nil, err
			}
			raw, err := io.ReadAll(io.LimitReader(rc, 4<<20))
			rc.Close()
			if err != nil {
				return nil, nil, err
			}
			n, err := ParseNuspec(raw)
			return n, raw, err
		}
	}
	return nil, nil, errors.New("no .nuspec in package")
}

// NormalizeVersion applies NuGet version normalisation: lowercase, numeric
// parts without leading zeros, a fourth ".0" dropped, build metadata removed.
func NormalizeVersion(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre, v = v[i:], v[:i]
	}
	parts := strings.Split(v, ".")
	for i, p := range parts {
		if n, err := strconv.Atoi(p); err == nil {
			parts[i] = strconv.Itoa(n)
		}
	}
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	if len(parts) == 4 && parts[3] == "0" {
		parts = parts[:3]
	}
	return strings.Join(parts, ".") + pre
}

// VersionLess orders NuGet versions (numeric parts, then SemVer prerelease).
func VersionLess(a, b string) bool {
	pa, ra := splitVersion(a)
	pb, rb := splitVersion(b)
	for i := 0; i < 4; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	if ra == "" || rb == "" {
		return ra != "" && rb == ""
	}
	as, bs := strings.Split(ra, "."), strings.Split(rb, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, xe := strconv.Atoi(as[i])
		y, ye := strconv.Atoi(bs[i])
		switch {
		case xe == nil && ye == nil:
			if x != y {
				return x < y
			}
		case xe == nil:
			return true
		case ye == nil:
			return false
		default:
			if as[i] != bs[i] {
				return as[i] < bs[i]
			}
		}
	}
	return len(as) < len(bs)
}

func splitVersion(v string) ([4]int, string) {
	var nums [4]int
	v = strings.ToLower(v)
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre, v = v[i+1:], v[:i]
	}
	for i, p := range strings.Split(v, ".") {
		if i >= 4 {
			break
		}
		nums[i], _ = strconv.Atoi(p)
	}
	return nums, pre
}

// IsPrerelease reports whether the version has a prerelease label.
func IsPrerelease(v string) bool {
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	return strings.Contains(v, "-")
}
