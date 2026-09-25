// Package vuln finds known vulnerabilities in stored packages by looking them
// up in OSV (osv.dev), and keeps the results.
package vuln

import (
	"encoding/json"
	"net/url"
	"strings"
)

// purlTypes maps a repository format to the package URL type OSV knows it by.
//
// Every entry here was checked against the live OSV API with a version known
// to be vulnerable. A format is left out when that could not be confirmed,
// because the failure mode of a wrong mapping is silent: OSV answers "no
// vulnerabilities" for a package it does not recognise, and the package would
// be reported clean. Left out, it is reported as not covered, which is true.
//
//   - apt, yum, alpine: OSV needs the distribution release to match an OS
//     package, which a repository of .deb/.rpm/.apk files does not record.
//   - conan: no vulnerable package could be found under ConanCenter to
//     confirm the lookup works.
//   - swift: OSV identifies Swift packages by repository URL, not registry
//     scope and name.
var purlTypes = map[string]string{
	"maven":    "maven",
	"npm":      "npm",
	"pypi":     "pypi",
	"go":       "golang",
	"nuget":    "nuget",
	"rubygems": "gem",
	"cargo":    "cargo",
	"composer": "composer",
	"pub":      "pub",
	"r":        "cran",
}

// Covered reports whether packages of this format can be checked at all.
func Covered(format string) bool {
	_, ok := purlTypes[format]
	return ok
}

// CoveredFormats lists the formats that can be checked.
func CoveredFormats() []string {
	out := make([]string, 0, len(purlTypes))
	for f := range purlTypes {
		out = append(out, f)
	}
	return out
}

// PURL builds the package URL OSV is queried with. ok is false when the
// format is not covered or this package lacks what its URL needs.
func PURL(format, namespace, name, version string, attrs json.RawMessage) (purl string, ok bool) {
	typ, covered := purlTypes[format]
	if !covered || name == "" || version == "" {
		return "", false
	}
	switch format {
	case "rubygems":
		// The namespace holds the platform (java, x86_64-linux…), which is
		// not part of a gem's identity as far as advisories go.
		namespace = ""
	case "r":
		// The namespace is the directory in the repository (src/contrib,
		// bin/windows/…). Left in, the purl names no package OSV knows.
		namespace = ""
	case "nuget":
		// NuGet ids are stored lower-cased, as the download paths are, but
		// OSV matches them case-sensitively: "newtonsoft.json" finds
		// nothing, "Newtonsoft.Json" finds the advisory. Without the id as
		// published the lookup would quietly come back clean.
		id := NuGetID(attrs)
		if id == "" || !strings.EqualFold(id, name) {
			return "", false
		}
		name = id
	}
	var b strings.Builder
	b.WriteString("pkg:")
	b.WriteString(typ)
	b.WriteByte('/')
	if namespace != "" {
		for _, seg := range strings.Split(namespace, "/") {
			b.WriteString(url.PathEscape(seg))
			b.WriteByte('/')
		}
	}
	b.WriteString(url.PathEscape(name))
	b.WriteByte('@')
	b.WriteString(url.PathEscape(version))
	return b.String(), true
}

// NuGetID returns a NuGet package's id with its published casing, when the
// package attributes carry it: from the .nuspec of a pushed package, or as
// resolved and recorded by the scanner for a proxied one.
func NuGetID(attrs json.RawMessage) string {
	var a struct {
		CanonicalID string `json:"canonicalId"`
		Nuspec      *struct {
			ID string `json:"id"`
		} `json:"nuspec"`
	}
	if json.Unmarshal(attrs, &a) != nil {
		return ""
	}
	if a.Nuspec != nil && a.Nuspec.ID != "" {
		return a.Nuspec.ID
	}
	return a.CanonicalID
}

// ScanEnabled reports whether a repository takes part in scanning. It does
// unless its settings say otherwise.
func ScanEnabled(attributes json.RawMessage) bool {
	var a struct {
		Vulnerabilities struct {
			Scan *bool `json:"scan"`
		} `json:"vulnerabilities"`
	}
	if json.Unmarshal(attributes, &a) != nil || a.Vulnerabilities.Scan == nil {
		return true
	}
	return *a.Vulnerabilities.Scan
}
