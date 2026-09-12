package apt

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Control is the parsed control file of a .deb plus computed fields.
type Control struct {
	Fields  map[string]string // ordered access via Keys
	Keys    []string
	Package string
	Version string
	Arch    string
}

// ParseControl parses an RFC822-style control block.
func ParseControl(b []byte) *Control {
	c := &Control{Fields: map[string]string{}}
	var last string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if last != "" {
				c.Fields[last] += "\n" + line
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if _, dup := c.Fields[k]; !dup {
			c.Keys = append(c.Keys, k)
		}
		c.Fields[k] = strings.TrimSpace(v)
		last = k
	}
	c.Package, c.Version, c.Arch = c.Fields["Package"], c.Fields["Version"], c.Fields["Architecture"]
	return c
}

// ReadDeb extracts the control file from a .deb (ar archive with
// debian-binary, control.tar.{gz,xz,zst}, data.tar.*).
func ReadDeb(r io.Reader) (*Control, error) {
	magic := make([]byte, 8)
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != "!<arch>\n" {
		return nil, errors.New("not an ar archive")
	}
	for {
		hdr := make([]byte, 60)
		if _, err := io.ReadFull(r, hdr); err != nil {
			if err == io.EOF {
				return nil, errors.New("control.tar not found in .deb")
			}
			return nil, err
		}
		name := strings.TrimSpace(string(hdr[0:16]))
		size, err := strconv.ParseInt(strings.TrimSpace(string(hdr[48:58])), 10, 64)
		if err != nil {
			return nil, errors.New("bad ar header")
		}
		body := io.LimitReader(r, size)
		if strings.HasPrefix(name, "control.tar") {
			var tr io.Reader
			switch {
			case strings.HasSuffix(name, ".gz"):
				gz, err := gzip.NewReader(body)
				if err != nil {
					return nil, err
				}
				tr = gz
			case strings.HasSuffix(name, ".xz"):
				xr, err := xz.NewReader(body)
				if err != nil {
					return nil, err
				}
				tr = xr
			case strings.HasSuffix(name, ".zst"):
				zr, err := zstd.NewReader(body)
				if err != nil {
					return nil, err
				}
				tr = zr
			default:
				tr = body
			}
			t := tar.NewReader(tr)
			for {
				th, err := t.Next()
				if err != nil {
					return nil, errors.New("control file not found")
				}
				if strings.TrimPrefix(th.Name, "./") == "control" {
					raw, err := io.ReadAll(io.LimitReader(t, 1<<20))
					if err != nil {
						return nil, err
					}
					return ParseControl(raw), nil
				}
			}
		}
		// Skip this member (ar pads to even sizes).
		if _, err := io.Copy(io.Discard, body); err != nil {
			return nil, err
		}
		if size%2 == 1 {
			io.CopyN(io.Discard, r, 1)
		}
	}
}

// Stanza renders the control fields for a Packages file, appending the
// repository-specific fields.
func (c *Control) Stanza(extra map[string]string, extraOrder []string) []byte {
	var b bytes.Buffer
	for _, k := range c.Keys {
		b.WriteString(k + ": " + c.Fields[k] + "\n")
	}
	for _, k := range extraOrder {
		if v, ok := extra[k]; ok {
			b.WriteString(k + ": " + v + "\n")
		}
	}
	b.WriteString("\n")
	return b.Bytes()
}
