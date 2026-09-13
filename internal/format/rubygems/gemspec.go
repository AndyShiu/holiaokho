package rubygems

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"regexp"
	"strings"
)

// GemInfo is what we need from a .gem's metadata.gz (YAML Gem::Specification).
type GemInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Platform     string   `json:"platform"`
	Summary      string   `json:"summary"`
	Authors      []string `json:"authors"`
	Licenses     []string `json:"licenses"`
	Homepage     string   `json:"homepage"`
	RequiredRuby string   `json:"requiredRuby"`
	RequiredRG   string   `json:"requiredRubygems"`
	// Dependencies are runtime deps as "name:req1&req2".
	Dependencies []Dep `json:"dependencies"`
}

type Dep struct {
	Name        string   `json:"name"`
	Requirement []string `json:"requirement"`
	Type        string   `json:"type"` // runtime|development
}

// ReadGem extracts metadata.gz from a .gem (tar with metadata.gz + data.tar.gz).
func ReadGem(data []byte) (*GemInfo, error) {
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("gem: metadata.gz not found")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == "metadata.gz" {
			gz, err := gzip.NewReader(tr)
			if err != nil {
				return nil, err
			}
			raw, err := io.ReadAll(io.LimitReader(gz, 8<<20))
			if err != nil {
				return nil, err
			}
			return parseSpecYAML(string(raw))
		}
	}
}

var (
	nameRe     = regexp.MustCompile(`(?m)^name:\s*(.+)$`)
	versionRe  = regexp.MustCompile(`(?ms)^version:.*?\n\s+version:\s*['"]?([^'"\n]+)`)
	platformRe = regexp.MustCompile(`(?m)^platform:\s*(.+)$`)
	summaryRe  = regexp.MustCompile(`(?m)^summary:\s*(.+)$`)
	homepageRe = regexp.MustCompile(`(?m)^homepage:\s*(.+)$`)
	depBlockRe = regexp.MustCompile(`(?ms)^dependencies:\n(.*?)^(?:[a-z_]+:)`)
	depRe      = regexp.MustCompile(`(?ms)- !ruby/object:Gem::Dependency\n\s+name:\s*(\S+)\n\s+requirement:.*?requirements:\n(.*?)\n\s+type:\s*(\S+)`)
	reqRe      = regexp.MustCompile(`- - "?([^"\n]+?)"?\n\s+- !ruby/object:Gem::Version\n\s+version:\s*['"]?([^'"\n]+)`)
	rubyReqRe  = regexp.MustCompile(`(?ms)^required_ruby_version:.*?requirements:\n\s+- - "?([^"\n]+?)"?\n\s+- !ruby/object:Gem::Version\n\s+version:\s*['"]?([^'"\n]+)`)
)

func yamlStr(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return s
}

// parseSpecYAML pulls the fields we need out of the Psych-emitted YAML
// without a full Ruby object model.
func parseSpecYAML(y string) (*GemInfo, error) {
	g := &GemInfo{Platform: "ruby"}
	if m := nameRe.FindStringSubmatch(y); m != nil {
		g.Name = yamlStr(m[1])
	}
	if m := versionRe.FindStringSubmatch(y); m != nil {
		g.Version = yamlStr(m[1])
	}
	if m := platformRe.FindStringSubmatch(y); m != nil {
		g.Platform = yamlStr(m[1])
	}
	if m := summaryRe.FindStringSubmatch(y); m != nil {
		g.Summary = yamlStr(m[1])
	}
	if m := homepageRe.FindStringSubmatch(y); m != nil {
		g.Homepage = yamlStr(m[1])
	}
	if m := rubyReqRe.FindStringSubmatch(y); m != nil {
		g.RequiredRuby = m[1] + " " + m[2]
	}
	for _, key := range []string{"authors", "licenses"} {
		re := regexp.MustCompile(`(?ms)^` + key + `:\n((?:\s*- .+\n)+)`)
		if m := re.FindStringSubmatch(y); m != nil {
			for _, line := range strings.Split(strings.TrimSpace(m[1]), "\n") {
				v := yamlStr(strings.TrimPrefix(strings.TrimSpace(line), "- "))
				if key == "authors" {
					g.Authors = append(g.Authors, v)
				} else {
					g.Licenses = append(g.Licenses, v)
				}
			}
		}
	}
	if m := depBlockRe.FindStringSubmatch(y + "\nend:"); m != nil {
		for _, d := range depRe.FindAllStringSubmatch(m[1], -1) {
			dep := Dep{Name: yamlStr(d[1]), Type: strings.TrimPrefix(yamlStr(d[3]), ":")}
			for _, r := range reqRe.FindAllStringSubmatch(d[2], -1) {
				dep.Requirement = append(dep.Requirement, r[1]+" "+r[2])
			}
			g.Dependencies = append(g.Dependencies, dep)
		}
	}
	if g.Name == "" || g.Version == "" {
		return nil, errors.New("gem: name/version not found in metadata")
	}
	return g, nil
}
