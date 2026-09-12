package rubygems

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"strings"
	"time"
)

// Minimal Ruby Marshal 4.8 writer for the legacy specs index:
// [[name, Gem::Version(version), platform], ...]

type marshalWriter struct {
	buf     bytes.Buffer
	symbols map[string]int
}

func newMarshal() *marshalWriter {
	m := &marshalWriter{symbols: map[string]int{}}
	m.buf.Write([]byte{4, 8})
	return m
}

func (m *marshalWriter) writeInt(n int) {
	switch {
	case n == 0:
		m.buf.WriteByte(0)
	case n > 0 && n < 123:
		m.buf.WriteByte(byte(n + 5))
	case n < 0 && n > -124:
		m.buf.WriteByte(byte(n - 5))
	default:
		var tmp [4]byte
		binary.LittleEndian.PutUint32(tmp[:], uint32(n))
		l := 4
		for l > 1 && ((n >= 0 && tmp[l-1] == 0) || (n < 0 && tmp[l-1] == 0xff)) {
			l--
		}
		if n >= 0 {
			m.buf.WriteByte(byte(l))
		} else {
			m.buf.WriteByte(byte(-l))
		}
		m.buf.Write(tmp[:l])
	}
}

func (m *marshalWriter) writeSymbol(s string) {
	if idx, ok := m.symbols[s]; ok {
		m.buf.WriteByte(';')
		m.writeInt(idx)
		return
	}
	m.symbols[s] = len(m.symbols)
	m.buf.WriteByte(':')
	m.writeInt(len(s))
	m.buf.WriteString(s)
}

// writeString writes a UTF-8 String with the encoding ivar (I"..." E true).
func (m *marshalWriter) writeString(s string) {
	m.buf.WriteByte('I')
	m.buf.WriteByte('"')
	m.writeInt(len(s))
	m.buf.WriteString(s)
	m.writeInt(1)
	m.writeSymbol("E")
	m.buf.WriteByte('T')
}

func (m *marshalWriter) writeVersion(v string) {
	// U:Gem::Version [ "v" ]
	m.buf.WriteByte('U')
	m.writeSymbol("Gem::Version")
	m.buf.WriteByte('[')
	m.writeInt(1)
	m.writeString(v)
}

// SpecsIndex renders specs.4.8.gz content for (name, version, platform) tuples.
func SpecsIndex(specs [][3]string) []byte {
	m := newMarshal()
	m.buf.WriteByte('[')
	m.writeInt(len(specs))
	for _, s := range specs {
		m.buf.WriteByte('[')
		m.writeInt(3)
		m.writeString(s[0])
		m.writeVersion(s[1])
		m.writeString(s[2])
	}
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write(m.buf.Bytes())
	w.Close()
	return gz.Bytes()
}

// --- Gem::Specification#_dump array, wrapped as u:Gem::Specification ---

func (m *marshalWriter) writeArray(n int) { m.buf.WriteByte('['); m.writeInt(n) }

// writeFixnum writes an Integer object (as opposed to a raw length/count).
func (m *marshalWriter) writeFixnum(n int) { m.buf.WriteByte('i'); m.writeInt(n) }

func (m *marshalWriter) writeTime(t time.Time) {
	t = t.UTC()
	p := uint32(1)<<31 | uint32(1)<<30 | uint32(t.Year()-1900)<<14 | uint32(t.Month()-1)<<10 | uint32(t.Day())<<5 | uint32(t.Hour())
	s := uint32(t.Minute())<<26 | uint32(t.Second())<<20 | uint32(t.Nanosecond()/1000)
	var raw [8]byte
	binary.LittleEndian.PutUint32(raw[0:4], p)
	binary.LittleEndian.PutUint32(raw[4:8], s)
	m.buf.WriteByte('u')
	m.writeSymbol("Time")
	m.writeInt(8)
	m.buf.Write(raw[:])
}

func (m *marshalWriter) writeRequirement(reqs []string) {
	if len(reqs) == 0 {
		reqs = []string{">= 0"}
	}
	m.buf.WriteByte('U')
	m.writeSymbol("Gem::Requirement")
	m.writeArray(1)
	m.writeArray(len(reqs))
	for _, r := range reqs {
		op, ver, ok := strings.Cut(strings.TrimSpace(r), " ")
		if !ok {
			op, ver = "=", op
		}
		m.writeArray(2)
		m.writeString(op)
		m.writeVersion(ver)
	}
}

func (m *marshalWriter) writeDependency(d Dep) {
	m.buf.WriteByte('o')
	m.writeSymbol("Gem::Dependency")
	m.writeInt(4)
	m.writeSymbol("@name")
	m.writeString(d.Name)
	m.writeSymbol("@requirement")
	m.writeRequirement(d.Requirement)
	m.writeSymbol("@type")
	typ := d.Type
	if typ == "" {
		typ = "runtime"
	}
	m.writeSymbol(typ)
	m.writeSymbol("@prerelease")
	m.buf.WriteByte('F')
}

func (m *marshalWriter) writeStrings(ss []string) {
	m.writeArray(len(ss))
	for _, s := range ss {
		m.writeString(s)
	}
}

func (m *marshalWriter) writeNil() { m.buf.WriteByte('0') }

// GemspecRz renders quick/Marshal.4.8/<gem>.gemspec.rz: zlib-deflated
// Marshal of the specification in RubyGems' _dump form.
func GemspecRz(g *GemInfo, date time.Time) []byte {
	inner := newMarshal()
	inner.writeArray(19)
	inner.writeString("3.4.0") // rubygems_version
	inner.writeFixnum(4)       // specification_version
	inner.writeString(g.Name)  // name
	inner.writeVersion(g.Version)
	inner.writeTime(date)
	inner.writeString(g.Summary)
	inner.writeRequirement(splitReq(g.RequiredRuby))
	inner.writeRequirement(splitReq(g.RequiredRG))
	plat := g.Platform
	if plat == "" {
		plat = "ruby"
	}
	inner.writeString(plat) // original_platform
	inner.writeArray(len(g.Dependencies))
	for _, d := range g.Dependencies {
		inner.writeDependency(d)
	}
	inner.writeString("") // rubyforge_project
	inner.writeNil()      // email
	inner.writeStrings(g.Authors)
	inner.writeString("") // description
	inner.writeString(g.Homepage)
	inner.buf.WriteByte('T') // has_rdoc
	inner.writeString(plat)  // new_platform
	inner.writeStrings(g.Licenses)
	inner.buf.WriteByte('{') // metadata
	inner.writeInt(0)

	outer := newMarshal()
	outer.buf.WriteByte('u')
	outer.writeSymbol("Gem::Specification")
	outer.writeInt(inner.buf.Len())
	outer.buf.Write(inner.buf.Bytes())

	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(outer.buf.Bytes())
	zw.Close()
	return z.Bytes()
}

func splitReq(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []string{s}
}
