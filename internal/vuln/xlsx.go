package vuln

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

// WriteXLSX writes the report as an Excel workbook: an overview sheet, one
// row per package with the version to upgrade to, and one row per
// vulnerability. Headers are frozen and filterable.
//
// The format is written directly — a workbook is a zip of a few XML files —
// rather than through a spreadsheet library and the dependencies it brings.
// Every value is an inline string or a number, never a formula, so text
// from an advisory cannot execute when the file is opened.
func WriteXLSX(w io.Writer, r *Report, lang string) error {
	L := func(k string) string { return label(lang, k) }
	sev := func(s string) string { return L(s) }

	overview := [][]cell{
		{bold(L("title"))},
		{},
		{txt(L("generated")), txt(r.GeneratedAt.Format("2006-01-02 15:04 UTC"))},
		{txt(L("source")), txt(r.Source)},
		{txt(L("version")), txt(r.Version)},
		{txt(L("filters")), txt(filterText(r.Filters, L("none")))},
		{},
		{bold(L("summary"))},
		{txt(L("packages")), num(float64(r.Summary.Packages))},
		{txt(L("vulnerabilities")), num(float64(r.Summary.Vulnerabilities))},
	}
	for _, s := range []string{SeverityCritical, SeverityHigh, SeverityModerate, SeverityLow, SeverityUnknown} {
		switch n := r.Summary.BySeverity[s]; {
		case !r.Filters.Includes(s):
			// Filtered out is not zero.
			overview = append(overview, []cell{txt("  " + sev(s)), txt("— (" + L("notInFilter") + ")")})
		case n > 0 || s != SeverityUnknown:
			overview = append(overview, []cell{txt("  " + sev(s)), num(float64(n))})
		}
	}
	if !r.Filters.Complete {
		overview = append(overview, []cell{bold(L("partial"))})
	}
	overview = append(overview, []cell{}, []cell{bold(L("coverage"))},
		[]cell{txt(L("scanned")), num(float64(r.Coverage.Scanned))},
		[]cell{txt(L("pending")), num(float64(r.Coverage.Pending))},
		[]cell{txt(L("notCovered")), num(float64(r.Coverage.NotCovered))},
		[]cell{txt(L("excluded")), num(float64(r.Coverage.Excluded))},
		[]cell{txt(L("lastScan")), txt(ts(r.Coverage.LastScan))},
		[]cell{}, []cell{txt(L("coverageNote"))},
	)

	pkgs := [][]cell{{hdr(L("package")), hdr(L("pkgVersion")), hdr(L("severity")), hdr(L("vulnerabilities")),
		hdr(L("upgradeTo")), hdr(L("fixesAll")), hdr(L("lastUsed")), hdr(L("repositories")), hdr("purl")}}
	vulns := [][]cell{{hdr(L("package")), hdr(L("pkgVersion")), hdr(L("severity")), hdr(L("cvss")), hdr(L("vulnerability")),
		hdr(L("aliases")), hdr(L("vulnSummary")), hdr(L("fixTo")), hdr(L("fixedIn")), hdr(L("published")),
		hdr(L("firstSeen")), hdr(L("repositories")), hdr(L("link"))}}
	for _, p := range r.Packages {
		fixes := L("yes")
		if !p.FixesAll {
			fixes = L("noFixFor")
		}
		pkgs = append(pkgs, []cell{txt(p.Name), txt(p.Version), txt(sev(p.Severity)), num(float64(len(p.Vulnerabilities))),
			txt(p.UpgradeTo), txt(fixes), txt(p.LastUsed.UTC().Format("2006-01-02")), txt(strings.Join(p.Repositories, ", ")), txt(p.PURL)})
		for _, v := range p.Vulnerabilities {
			sc := txt("")
			if v.Score != nil {
				sc = num(*v.Score)
			}
			vulns = append(vulns, []cell{txt(p.Name), txt(p.Version), txt(sev(v.Severity)), sc, txt(v.ID),
				txt(strings.Join(v.Aliases, ", ")), txt(v.Summary), txt(v.FixTo), txt(strings.Join(v.FixedIn, ", ")),
				txt(date(v.Published)), txt(v.FirstSeen.UTC().Format("2006-01-02")), txt(strings.Join(p.Repositories, ", ")), txt(v.URL)})
		}
	}

	sheets := []sheet{
		{L("summary"), overview, []float64{34, 40}, false},
		{L("sheetPackages"), pkgs, []float64{44, 16, 12, 10, 16, 20, 14, 28, 60}, true},
		{L("sheetFindings"), vulns, []float64{40, 14, 12, 8, 22, 22, 60, 16, 26, 12, 12, 28, 46}, true},
	}
	return writeWorkbook(w, sheets)
}

func filterText(f Filters, none string) string {
	var parts []string
	add := func(prefix, v string) {
		if v != "" {
			parts = append(parts, prefix+v)
		}
	}
	add("severity >= ", f.MinSeverity)
	add("severity = ", f.Severity)
	add("repository = ", f.Repository)
	add("format = ", f.Format)
	add("search = ", f.Query)
	if len(parts) == 0 {
		return none
	}
	return strings.Join(parts, ", ")
}

func ts(t *time.Time) string {
	if t == nil {
		return "—"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// ------------------------------------------------------- minimal writer

type cell struct {
	s     string
	n     float64
	isNum bool
	style int // 0 plain, 1 bold, 2 header
}

func txt(s string) cell  { return cell{s: s} }
func bold(s string) cell { return cell{s: s, style: 1} }
func hdr(s string) cell  { return cell{s: s, style: 2} }
func num(n float64) cell { return cell{n: n, isNum: true} }

type sheet struct {
	name   string
	rows   [][]cell
	widths []float64
	table  bool // first row is a header: freeze it and add a filter
}

func writeWorkbook(w io.Writer, sheets []sheet) error {
	z := zip.NewWriter(w)
	put := func(name, body string) error {
		f, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, xml.Header+body)
		return err
	}
	var ct, rels, list strings.Builder
	for i := range sheets {
		n := i + 1
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
		fmt.Fprintf(&list, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc(sheetName(sheets[i].name)), n, n)
	}
	styles := len(sheets) + 1
	steps := []struct{ name, body string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` + ct.String() + `</Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` + list.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, styles) + `</Relationships>`},
		// Three cell styles: plain, bold, and a shaded bold header.
		{"xl/styles.xml", `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FFECE8E0"/></patternFill></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf/></cellStyleXfs><cellXfs count="3"><xf fontId="0" fillId="0" borderId="0" xfId="0"><alignment vertical="top" wrapText="1"/></xf><xf fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/><xf fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`},
	}
	for _, s := range steps {
		if err := put(s.name, s.body); err != nil {
			return err
		}
	}
	for i, sh := range sheets {
		if err := put(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sheetXML(sh)); err != nil {
			return err
		}
	}
	return z.Close()
}

func sheetXML(sh sheet) string {
	var b strings.Builder
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	if sh.table {
		b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	}
	if len(sh.widths) > 0 {
		b.WriteString(`<cols>`)
		for i, wd := range sh.widths {
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%g" customWidth="1"/>`, i+1, i+1, wd)
		}
		b.WriteString(`</cols>`)
	}
	b.WriteString(`<sheetData>`)
	maxCol := 0
	for r, row := range sh.rows {
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, x := range row {
			ref := colName(c) + fmt.Sprint(r+1)
			if x.isNum {
				fmt.Fprintf(&b, `<c r="%s" s="%d"><v>%g</v></c>`, ref, x.style, x.n)
			} else if x.s != "" {
				fmt.Fprintf(&b, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, x.style, esc(x.s))
			}
			maxCol = max(maxCol, c+1)
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	if sh.table && len(sh.rows) > 0 && maxCol > 0 {
		fmt.Fprintf(&b, `<autoFilter ref="A1:%s%d"/>`, colName(maxCol-1), len(sh.rows))
	}
	b.WriteString(`</worksheet>`)
	return b.String()
}

func colName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// esc escapes text for XML and drops the control characters XML 1.0 cannot
// hold at all, which would otherwise make Excel refuse the whole file.
func esc(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, s)
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// sheetName fits Excel's rules: at most 31 characters, none of []:*?/\.
func sheetName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, s)
	if rs := []rune(s); len(rs) > 31 {
		s = string(rs[:31])
	}
	return s
}
