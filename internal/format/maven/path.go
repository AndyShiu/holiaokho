package maven

import (
	"path"
	"regexp"
	"strings"
)

// Coordinates parsed from a Maven 2 layout path.
type Coordinates struct {
	GroupID    string
	ArtifactID string
	Version    string // may be "1.0-SNAPSHOT"
	// BaseVersion is Version with a timestamped snapshot collapsed to -SNAPSHOT.
	BaseVersion string
	Classifier  string
	Extension   string
	// Snapshot is true for -SNAPSHOT base versions.
	Snapshot bool
	// Timestamp / BuildNumber set for timestamped snapshot files.
	Timestamp   string
	BuildNumber string
	// Checksum is the trailing checksum extension (sha1/md5/sha256/sha512) or "".
	Checksum string
	// Signature is true for .asc files.
	Signature bool
	// FileName is the last path segment.
	FileName string
}

var (
	checksumExts = map[string]bool{"sha1": true, "md5": true, "sha256": true, "sha512": true}
	snapshotTSRe = regexp.MustCompile(`^(\d{8}\.\d{6})-(\d+)$`)
	metadataName = "maven-metadata.xml"
)

// IsMetadata reports whether the path is maven-metadata.xml or one of its
// checksum/signature side files.
func IsMetadata(p string) bool {
	base := path.Base(p)
	base = stripSide(base)
	return base == metadataName
}

func stripSide(name string) string {
	for {
		ext := strings.ToLower(path.Ext(name))
		if ext == "" {
			return name
		}
		e := ext[1:]
		if checksumExts[e] || e == "asc" {
			name = strings.TrimSuffix(name, ext)
			continue
		}
		return name
	}
}

// IsChecksum reports whether the path ends with a checksum extension.
func IsChecksum(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	return ext != "" && checksumExts[ext[1:]]
}

// IsSnapshotPath reports whether the version directory ends with -SNAPSHOT.
func IsSnapshotPath(p string) bool {
	dir := path.Dir(p)
	return strings.HasSuffix(dir, "-SNAPSHOT")
}

// ParsePath parses an artifact path. It returns nil for paths that are not
// artifacts (metadata, directories, malformed).
func ParsePath(p string) *Coordinates {
	p = strings.Trim(p, "/")
	segs := strings.Split(p, "/")
	if len(segs) < 4 {
		return nil
	}
	file := segs[len(segs)-1]
	version := segs[len(segs)-2]
	artifact := segs[len(segs)-3]
	group := strings.Join(segs[:len(segs)-3], ".")
	if stripSide(file) == metadataName {
		return nil
	}
	c := &Coordinates{GroupID: group, ArtifactID: artifact, Version: version, BaseVersion: version, FileName: file}
	name := file
	// Peel side extensions.
	for {
		ext := strings.ToLower(path.Ext(name))
		if ext == "" {
			break
		}
		e := ext[1:]
		if checksumExts[e] {
			if c.Checksum != "" {
				return nil
			}
			c.Checksum = e
			name = strings.TrimSuffix(name, ext)
			continue
		}
		if e == "asc" {
			c.Signature = true
			name = strings.TrimSuffix(name, ext)
			continue
		}
		break
	}
	prefix := artifact + "-"
	if !strings.HasPrefix(name, prefix) {
		return nil
	}
	rest := name[len(prefix):]
	// rest = <version>[-classifier].<ext>  where version may be timestamped.
	dot := strings.LastIndexByte(rest, '.')
	if dot <= 0 {
		return nil
	}
	c.Extension = rest[dot+1:]
	stem := rest[:dot]
	// Handle multi-part extensions like tar.gz.
	if strings.HasSuffix(stem, ".tar") {
		c.Extension = "tar." + c.Extension
		stem = strings.TrimSuffix(stem, ".tar")
	}
	if strings.HasSuffix(version, "-SNAPSHOT") {
		c.Snapshot = true
		base := strings.TrimSuffix(version, "-SNAPSHOT")
		switch {
		case strings.HasPrefix(stem, version):
			// non-timestamped: artifact-1.0-SNAPSHOT[-classifier]
			c.Classifier = strings.TrimPrefix(strings.TrimPrefix(stem, version), "-")
		case strings.HasPrefix(stem, base+"-"):
			tail := stem[len(base)+1:]
			// tail = 20260913.101010-3[-classifier]
			parts := strings.SplitN(tail, "-", 3)
			if len(parts) < 2 {
				return nil
			}
			ts := parts[0] + "-" + parts[1]
			m := snapshotTSRe.FindStringSubmatch(ts)
			if m == nil {
				return nil
			}
			c.Timestamp, c.BuildNumber = m[1], m[2]
			c.Version = base + "-" + ts
			if len(parts) == 3 {
				c.Classifier = parts[2]
			}
		default:
			return nil
		}
		return c
	}
	if !strings.HasPrefix(stem, version) {
		return nil
	}
	c.Classifier = strings.TrimPrefix(strings.TrimPrefix(stem, version), "-")
	return c
}

// ContentType guesses a MIME type from the file name.
func ContentType(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".jar", ".war", ".ear":
		return "application/java-archive"
	case ".pom", ".xml":
		return "application/xml"
	case ".sha1", ".md5", ".sha256", ".sha512", ".asc", ".txt":
		return "text/plain"
	case ".zip":
		return "application/zip"
	case ".gz", ".tgz":
		return "application/gzip"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}
