package yum

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// RPM header parsing (RPM package format v3/v4): lead (96 bytes), signature
// header (aligned to 8), then the main header with tag/type/offset/count
// index entries and a data store.

const (
	tagName        = 1000
	tagVersion     = 1001
	tagRelease     = 1002
	tagEpoch       = 1003
	tagSummary     = 1004
	tagDescription = 1005
	tagBuildTime   = 1006
	tagBuildHost   = 1007
	tagSize        = 1009
	tagLicense     = 1014
	tagGroup       = 1016
	tagURL         = 1020
	tagArch        = 1022
	tagSourceRPM   = 1044
	tagProvideName = 1047
	tagRequireFlag = 1048
	tagRequireName = 1049
	tagRequireVer  = 1050
	tagConflName   = 1054
	tagObsolName   = 1090
	tagProvideFlag = 1112
	tagProvideVer  = 1113
	tagDirIndexes  = 1116
	tagBaseNames   = 1117
	tagDirNames    = 1118
	tagVendor      = 1011
	tagPackager    = 1015
	tagFileModes   = 1030
	tagFileFlags   = 1037
)

type rpmHeader struct {
	entries map[int]rpmEntry
	store   []byte
}

type rpmEntry struct {
	typ    uint32
	offset uint32
	count  uint32
}

func readHeader(r io.Reader) (*rpmHeader, int, error) {
	magic := make([]byte, 16)
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, 0, err
	}
	if magic[0] != 0x8e || magic[1] != 0xad || magic[2] != 0xe8 {
		return nil, 0, errors.New("bad rpm header magic")
	}
	nindex := binary.BigEndian.Uint32(magic[8:12])
	hsize := binary.BigEndian.Uint32(magic[12:16])
	if nindex > 65536 || hsize > 64<<20 {
		return nil, 0, errors.New("rpm header too large")
	}
	index := make([]byte, 16*nindex)
	if _, err := io.ReadFull(r, index); err != nil {
		return nil, 0, err
	}
	store := make([]byte, hsize)
	if _, err := io.ReadFull(r, store); err != nil {
		return nil, 0, err
	}
	h := &rpmHeader{entries: map[int]rpmEntry{}, store: store}
	for i := 0; i < int(nindex); i++ {
		e := index[i*16:]
		tag := int(binary.BigEndian.Uint32(e[0:4]))
		h.entries[tag] = rpmEntry{typ: binary.BigEndian.Uint32(e[4:8]), offset: binary.BigEndian.Uint32(e[8:12]), count: binary.BigEndian.Uint32(e[12:16])}
	}
	return h, 16 + len(index) + len(store), nil
}

func (h *rpmHeader) strings(tag int) []string {
	e, ok := h.entries[tag]
	if !ok || int(e.offset) > len(h.store) {
		return nil
	}
	switch e.typ {
	case 6, 8, 9: // STRING, STRING_ARRAY, I18NSTRING
		var out []string
		p := int(e.offset)
		for i := 0; i < int(e.count) && p < len(h.store); i++ {
			end := bytes.IndexByte(h.store[p:], 0)
			if end < 0 {
				end = len(h.store) - p
			}
			out = append(out, string(h.store[p:p+end]))
			p += end + 1
			if e.typ == 6 {
				break
			}
		}
		return out
	}
	return nil
}

func (h *rpmHeader) str(tag int) string {
	s := h.strings(tag)
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func (h *rpmHeader) ints(tag int) []int64 {
	e, ok := h.entries[tag]
	if !ok {
		return nil
	}
	var out []int64
	p := int(e.offset)
	for i := 0; i < int(e.count); i++ {
		switch e.typ {
		case 2: // INT8
			if p+1 > len(h.store) {
				return out
			}
			out = append(out, int64(h.store[p]))
			p++
		case 3: // INT16
			if p+2 > len(h.store) {
				return out
			}
			out = append(out, int64(binary.BigEndian.Uint16(h.store[p:])))
			p += 2
		case 4: // INT32
			if p+4 > len(h.store) {
				return out
			}
			out = append(out, int64(binary.BigEndian.Uint32(h.store[p:])))
			p += 4
		case 5: // INT64
			if p+8 > len(h.store) {
				return out
			}
			out = append(out, int64(binary.BigEndian.Uint64(h.store[p:])))
			p += 8
		default:
			return out
		}
	}
	return out
}

func (h *rpmHeader) int(tag int) int64 {
	v := h.ints(tag)
	if len(v) == 0 {
		return 0
	}
	return v[0]
}

// Package is the metadata extracted from an RPM for repodata generation.
type Package struct {
	Name, Version, Release, Arch, Epoch                                    string
	Summary, Description, License, Group, URL, Vendor, Packager, BuildHost string
	BuildTime, InstalledSize                                               int64
	SourceRPM                                                              string
	Provides, Requires, Conflicts, Obsoletes                               []Dep
	Files                                                                  []string
	Dirs                                                                   []string
	HeaderStart, HeaderEnd                                                 int64
}

type Dep struct {
	Name, Flags, Epoch, Version, Release string
	Pre                                  bool
}

func flagsStr(f int64) string {
	switch f & 0xf {
	case 2:
		return "LT"
	case 4:
		return "GT"
	case 8:
		return "EQ"
	case 10:
		return "LE"
	case 12:
		return "GE"
	}
	return ""
}

func splitEVR(evr string) (e, v, r string) {
	if i := strings.IndexByte(evr, ':'); i >= 0 {
		e, evr = evr[:i], evr[i+1:]
	}
	if i := strings.LastIndexByte(evr, '-'); i >= 0 {
		return e, evr[:i], evr[i+1:]
	}
	return e, evr, ""
}

func (h *rpmHeader) deps(nameTag, flagTag, verTag int) []Dep {
	names, flags, vers := h.strings(nameTag), h.ints(flagTag), h.strings(verTag)
	var out []Dep
	for i, n := range names {
		d := Dep{Name: n}
		if i < len(flags) {
			d.Flags = flagsStr(flags[i])
			d.Pre = flags[i]&(1<<9|1<<10|1<<11|1<<12|1<<13|1<<24|1<<25) != 0
		}
		if i < len(vers) && vers[i] != "" {
			d.Epoch, d.Version, d.Release = splitEVR(vers[i])
			if d.Epoch == "" {
				d.Epoch = "0"
			}
		}
		out = append(out, d)
	}
	return out
}

// ReadRPM parses the lead, signature and main header of an RPM.
func ReadRPM(r io.Reader) (*Package, error) {
	lead := make([]byte, 96)
	if _, err := io.ReadFull(r, lead); err != nil {
		return nil, err
	}
	if lead[0] != 0xed || lead[1] != 0xab || lead[2] != 0xee || lead[3] != 0xdb {
		return nil, errors.New("not an rpm")
	}
	sig, n, err := readHeader(r)
	if err != nil {
		return nil, fmt.Errorf("signature header: %w", err)
	}
	_ = sig
	pos := int64(96 + n)
	if pad := n % 8; pad != 0 {
		io.CopyN(io.Discard, r, int64(8-pad))
		pos += int64(8 - pad)
	}
	hdr, hn, err := readHeader(r)
	if err != nil {
		return nil, fmt.Errorf("main header: %w", err)
	}
	p := &Package{
		Name: hdr.str(tagName), Version: hdr.str(tagVersion), Release: hdr.str(tagRelease), Arch: hdr.str(tagArch),
		Summary: hdr.str(tagSummary), Description: hdr.str(tagDescription), License: hdr.str(tagLicense), Group: hdr.str(tagGroup),
		URL: hdr.str(tagURL), Vendor: hdr.str(tagVendor), Packager: hdr.str(tagPackager), BuildHost: hdr.str(tagBuildHost),
		BuildTime: hdr.int(tagBuildTime), InstalledSize: hdr.int(tagSize), SourceRPM: hdr.str(tagSourceRPM),
		HeaderStart: pos, HeaderEnd: pos + int64(hn),
	}
	if e := hdr.ints(tagEpoch); len(e) > 0 {
		p.Epoch = fmt.Sprint(e[0])
	} else {
		p.Epoch = "0"
	}
	if p.SourceRPM == "" {
		p.Arch = "src"
	}
	p.Provides = hdr.deps(tagProvideName, tagProvideFlag, tagProvideVer)
	p.Requires = hdr.deps(tagRequireName, tagRequireFlag, tagRequireVer)
	p.Conflicts = hdr.deps(tagConflName, 1053, 1055)
	p.Obsoletes = hdr.deps(tagObsolName, 1114, 1115)
	dirs, bases, idx := hdr.strings(tagDirNames), hdr.strings(tagBaseNames), hdr.ints(tagDirIndexes)
	modes := hdr.ints(tagFileModes)
	for i, b := range bases {
		if i < len(idx) && int(idx[i]) < len(dirs) {
			full := dirs[idx[i]] + b
			if i < len(modes) && modes[i]&0o40000 != 0 {
				p.Dirs = append(p.Dirs, full)
			} else {
				p.Files = append(p.Files, full)
			}
		}
	}
	return p, nil
}
