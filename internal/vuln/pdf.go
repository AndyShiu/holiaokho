package vuln

import (
	"embed"
	"fmt"
	"io"
	"strings"

	"codeberg.org/go-pdf/fpdf"
)

//go:embed fonts/*.ttf
var pdfFonts embed.FS

// fontFor is the embedded font family a report language is set in. English
// uses the Traditional Chinese cut, which carries the same Latin glyphs.
func fontFor(lang string) string {
	switch lang {
	case "zh-TW", "zh-CN", "ja", "ko":
		return lang
	}
	return "zh-TW"
}

// pdfColour is the severity palette of the web UI, so a printed report and
// the screen agree: [fill, text, border] as RGB.
var pdfColour = map[string][3][3]int{
	SeverityCritical: {{209, 67, 67}, {255, 255, 255}, {209, 67, 67}},
	SeverityHigh:     {{251, 234, 234}, {209, 67, 67}, {209, 67, 67}},
	SeverityModerate: {{251, 243, 220}, {30, 42, 59}, {212, 160, 23}},
	SeverityLow:      {{255, 255, 255}, {43, 87, 168}, {43, 87, 168}},
	SeverityUnknown:  {{255, 255, 255}, {138, 148, 163}, {201, 196, 185}},
}

const (
	pageW, pageH = 210.0, 297.0 // A4, mm
	margin       = 14.0
	contentW     = pageW - 2*margin
)

// WritePDF writes the report for reading and printing: an overview page,
// then each affected package with the version to upgrade to and the
// vulnerabilities behind it, worst first.
func WritePDF(w io.Writer, r *Report, lang string) error {
	L := func(k string) string { return label(lang, k) }
	family := fontFor(lang)
	pdf := fpdf.New("P", "mm", "A4", "")
	for _, style := range []struct{ fpdfStyle, file string }{{"", "regular"}, {"B", "bold"}} {
		b, err := pdfFonts.ReadFile("fonts/" + family + "-" + style.file + ".ttf")
		if err != nil {
			return err
		}
		pdf.AddUTF8FontFromBytes(family, style.fpdfStyle, b)
	}
	pdf.SetMargins(margin, margin, margin)
	pdf.SetAutoPageBreak(false, margin)
	pdf.SetTitle(L("title"), true)
	pdf.SetCreator("Holiaokho "+r.Version, true)
	pdf.AliasNbPages("{nb}")
	pdf.SetFooterFunc(func() {
		pdf.SetY(pageH - 10)
		pdf.SetFont(family, "", 7.5)
		pdf.SetTextColor(138, 148, 163)
		pdf.CellFormat(contentW/2, 4, L("version")+" · "+L("title"), "", 0, "L", false, 0, "")
		pdf.CellFormat(contentW/2, 4, fmt.Sprintf("%s %d / {nb}", L("page"), pdf.PageNo()), "", 0, "R", false, 0, "")
	})

	ink := func() { pdf.SetTextColor(30, 42, 59) }
	muted := func() { pdf.SetTextColor(91, 102, 118) }
	text := func(s string) string { return pdfSafe(s) }

	// ---- overview
	pdf.AddPage()
	pdf.SetFillColor(232, 150, 58) // the amber bar, as on every page of the site
	pdf.Rect(margin, margin, 12, 1.1, "F")
	pdf.SetY(margin + 5)
	pdf.SetFont(family, "B", 20)
	ink()
	pdf.CellFormat(contentW, 10, L("title"), "", 1, "L", false, 0, "")
	pdf.SetFont(family, "", 9)
	muted()
	pdf.CellFormat(contentW, 5, fmt.Sprintf("%s %s  ·  %s %s  ·  %s %s", L("generated"), r.GeneratedAt.Format("2006-01-02 15:04 UTC"),
		L("source"), r.Source, L("version"), r.Version), "", 1, "L", false, 0, "")
	filters := L("none") // a label, so not passed through pdfSafe
	if f := filterText(r.Filters, ""); f != "" {
		filters = text(f)
	}
	pdf.CellFormat(contentW, 5, L("filters")+": "+filters, "", 1, "L", false, 0, "")
	pdf.Ln(6)

	// counts
	sec := func(title string) {
		pdf.SetFont(family, "B", 11)
		ink()
		pdf.CellFormat(contentW, 7, title, "", 1, "L", false, 0, "")
	}
	sec(L("summary"))
	boxW := (contentW - 3*4) / 4
	y := pdf.GetY() + 1
	for i, s := range []string{SeverityCritical, SeverityHigh, SeverityModerate, SeverityLow} {
		x := margin + float64(i)*(boxW+4)
		pdf.SetDrawColor(227, 223, 214)
		pdf.RoundedRect(x, y, boxW, 20, 1.6, "1234", "D")
		tag(pdf, family, x+3, y+3, L(s), s)
		pdf.SetXY(x+3, y+9.5)
		if r.Filters.Includes(s) {
			pdf.SetFont(family, "B", 16)
			ink()
			pdf.CellFormat(boxW-6, 8, fmt.Sprint(r.Summary.BySeverity[s]), "", 0, "L", false, 0, "")
		} else {
			// Filtered out is not zero: say so instead of printing a 0.
			pdf.SetFont(family, "B", 16)
			pdf.SetTextColor(201, 196, 185)
			pdf.CellFormat(12, 8, "—", "", 0, "L", false, 0, "")
			pdf.SetFont(family, "", 7.5)
			muted()
			pdf.CellFormat(boxW-18, 8, L("notInFilter"), "", 0, "L", false, 0, "")
		}
	}
	pdf.SetY(y + 24)
	if !r.Filters.Complete {
		pdf.SetFont(family, "B", 9)
		pdf.SetTextColor(138, 106, 11)
		pdf.MultiCell(contentW, 5, L("partial"), "", "L", false)
		pdf.Ln(1)
	}
	pdf.SetFont(family, "", 9.5)
	ink()
	pdf.CellFormat(contentW, 5, fmt.Sprintf("%s: %d    %s: %d", L("packages"), r.Summary.Packages, L("vulnerabilities"), r.Summary.Vulnerabilities), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	sec(L("coverage"))
	pdf.SetFont(family, "", 9.5)
	last := "—"
	if r.Coverage.LastScan != nil {
		last = r.Coverage.LastScan.UTC().Format("2006-01-02 15:04 UTC")
	}
	for _, kv := range [][2]string{
		{L("scanned"), fmt.Sprint(r.Coverage.Scanned)}, {L("pending"), fmt.Sprint(r.Coverage.Pending)},
		{L("notCovered"), fmt.Sprint(r.Coverage.NotCovered)}, {L("excluded"), fmt.Sprint(r.Coverage.Excluded)},
		{L("lastScan"), last},
	} {
		muted()
		pdf.CellFormat(70, 5, kv[0], "", 0, "L", false, 0, "")
		ink()
		pdf.CellFormat(contentW-70, 5, kv[1], "", 1, "L", false, 0, "")
	}
	pdf.Ln(1)
	pdf.SetFont(family, "", 8.5)
	muted()
	pdf.MultiCell(contentW, 4.4, L("coverageNote"), "", "L", false)

	if len(r.Packages) == 0 {
		pdf.Ln(6)
		pdf.SetFont(family, "B", 11)
		ink()
		pdf.MultiCell(contentW, 6, L("empty"), "", "L", false)
		return pdf.Output(w)
	}

	// ---- package overview: the page someone reads to find out whether
	// their project is affected and what to move to.
	pdf.Ln(6)
	sec(L("sheetPackages"))
	oSev, oVer, oN, oUsed, oUp := 26.0, 26.0, 14.0, 24.0, 34.0
	oName := contentW - oSev - oVer - oN - oUsed - oUp
	head := func() {
		pdf.SetFont(family, "B", 8)
		muted()
		pdf.SetFillColor(246, 244, 239)
		for _, c := range []struct {
			w     float64
			s, al string
		}{{oSev, L("severity"), "L"}, {oName, L("package"), "L"}, {oVer, L("pkgVersion"), "L"}, {oN, L("vulnerabilities"), "R"}, {oUsed, L("lastUsed"), "R"}, {oUp, L("upgradeTo"), "R"}} {
			pdf.CellFormat(c.w, 6, " "+c.s+" ", "", 0, c.al, true, 0, "")
		}
		pdf.Ln(6)
	}
	head()
	for _, p := range r.Packages {
		if pdf.GetY()+6.5 > pageH-margin-8 {
			pdf.AddPage()
			head()
		}
		y := pdf.GetY()
		tag(pdf, family, margin+1, y+0.8, L(p.Severity), p.Severity)
		pdf.SetXY(margin+oSev, y)
		pdf.SetFont(family, "", 8.5)
		ink()
		pdf.CellFormat(oName, 6.5, text(truncate(pdf, p.Name, oName-2)), "", 0, "L", false, 0, "")
		pdf.CellFormat(oVer, 6.5, text(p.Version), "", 0, "L", false, 0, "")
		pdf.CellFormat(oN, 6.5, fmt.Sprint(len(p.Vulnerabilities)), "", 0, "R", false, 0, "")
		muted()
		pdf.CellFormat(oUsed, 6.5, p.LastUsed.UTC().Format("2006-01-02"), "", 0, "R", false, 0, "")
		up := p.UpgradeTo
		if up == "" {
			up = "—"
		} else if !p.FixesAll {
			up += " *"
		}
		pdf.SetFont(family, "B", 8.5)
		pdf.SetTextColor(46, 158, 107)
		if p.Malicious {
			up = L("remove") + " †"
			pdf.SetTextColor(200, 50, 40)
		}
		pdf.CellFormat(oUp, 6.5, text(up), "", 1, "R", false, 0, "")
		pdf.SetDrawColor(236, 232, 224)
		pdf.Line(margin, y+6.5, margin+contentW, y+6.5)
	}
	for _, p := range r.Packages {
		if p.Malicious { // the footnote only when a † is shown
			pdf.SetFont(family, "", 7.5)
			muted()
			pdf.Ln(1.5)
			pdf.CellFormat(contentW, 4, "† "+L("maliciousNote"), "", 1, "L", false, 0, "")
			break
		}
	}
	for _, p := range r.Packages {
		if p.UpgradeTo != "" && !p.FixesAll { // the footnote only when a * is shown
			pdf.SetFont(family, "", 7.5)
			muted()
			pdf.Ln(1.5)
			pdf.CellFormat(contentW, 4, "* "+L("noFixFor"), "", 1, "L", false, 0, "")
			break
		}
	}

	// ---- packages in detail
	pdf.AddPage()
	colSev, colID, colFix := 24.0, 42.0, 30.0
	colSum := contentW - colSev - colID - colFix
	lineH := 4.2
	for _, p := range r.Packages {
		// A package's heading never sits alone at the foot of a page.
		if pdf.GetY()+30 > pageH-margin-8 {
			pdf.AddPage()
		}
		top := pdf.GetY()
		pdf.SetFillColor(246, 244, 239)
		pdf.Rect(margin, top, contentW, 15, "F")
		tag(pdf, family, margin+3, top+3, L(p.Severity), p.Severity)
		pdf.SetXY(margin+3+tagWidth(pdf, family, L(p.Severity))+3, top+2.2)
		pdf.SetFont(family, "B", 11)
		ink()
		nameW := contentW - 70
		pdf.CellFormat(nameW, 6, text(truncate(pdf, p.Name+" "+p.Version, nameW)), "", 0, "L", false, 0, "")
		pdf.SetXY(margin+contentW-66, top+2.2)
		pdf.SetFont(family, "B", 9.5)
		if p.UpgradeTo != "" {
			pdf.SetTextColor(46, 158, 107)
			pdf.CellFormat(63, 6, L("upgradeTo")+" "+text(p.UpgradeTo), "", 0, "R", false, 0, "")
		}
		pdf.SetXY(margin+3, top+8.6)
		pdf.SetFont(family, "", 7.5)
		muted()
		meta := strings.Join(p.Repositories, ", ") + "  ·  " + p.PURL
		if !p.FixesAll {
			meta = L("noFixFor") + "  ·  " + meta
		}
		pdf.CellFormat(contentW-6, 4, text(truncate(pdf, meta, contentW-6)), "", 0, "L", false, 0, "")
		pdf.SetY(top + 17)

		for _, v := range p.Vulnerabilities {
			pdf.SetFont(family, "", 8.5)
			sum := pdf.SplitText(text(v.Summary), colSum-2)
			if len(sum) == 0 {
				sum = []string{"—"}
			}
			h := max(float64(len(sum))*lineH, 2*lineH) + 3
			if pdf.GetY()+h > pageH-margin-8 {
				pdf.AddPage()
			}
			rowY := pdf.GetY()
			tag(pdf, family, margin+1, rowY+1.2, scoreLabel(L(v.Severity), v.Score), v.Severity)

			pdf.SetXY(margin+colSev, rowY+1)
			pdf.SetFont(family, "", 8.5)
			ink()
			pdf.CellFormat(colID, lineH, text(v.ID), "", 2, "L", false, 0, v.URL)
			pdf.SetX(margin + colSev)
			pdf.SetFont(family, "", 7.5)
			muted()
			pdf.CellFormat(colID, lineH, text(cveOf(v)), "", 0, "L", false, 0, "")

			pdf.SetXY(margin+colSev+colID, rowY+1)
			pdf.SetFont(family, "", 8.5)
			ink()
			for _, line := range sum {
				pdf.SetX(margin + colSev + colID)
				pdf.CellFormat(colSum, lineH, line, "", 2, "L", false, 0, "")
			}

			pdf.SetXY(margin+colSev+colID+colSum, rowY+1)
			fix := v.FixTo
			switch {
			case v.Malicious:
				fix = L("remove")
				pdf.SetTextColor(200, 50, 40)
			case fix == "":
				fix = "—"
				muted()
			default:
				pdf.SetTextColor(46, 158, 107)
			}
			pdf.SetFont(family, "B", 8.5)
			pdf.CellFormat(colFix, lineH, text(fix), "", 0, "R", false, 0, "")

			pdf.SetY(rowY + h)
			pdf.SetDrawColor(227, 223, 214)
			pdf.Line(margin, rowY+h-0.5, margin+contentW, rowY+h-0.5)
		}
		pdf.Ln(5)
	}
	return pdf.Output(w)
}

// tag draws a severity label like the web UI's.
func tag(pdf *fpdf.Fpdf, family string, x, y float64, s, severity string) {
	c, ok := pdfColour[severity]
	if !ok {
		c = pdfColour[SeverityUnknown]
	}
	pdf.SetFont(family, "B", 7.5)
	w := tagWidth(pdf, family, s)
	pdf.SetFillColor(c[0][0], c[0][1], c[0][2])
	pdf.SetDrawColor(c[2][0], c[2][1], c[2][2])
	pdf.RoundedRect(x, y, w, 5, 1, "1234", "FD")
	pdf.SetTextColor(c[1][0], c[1][1], c[1][2])
	pdf.SetXY(x, y)
	pdf.CellFormat(w, 5, s, "", 0, "C", false, 0, "")
}

func tagWidth(pdf *fpdf.Fpdf, family, s string) float64 {
	pdf.SetFont(family, "B", 7.5)
	return pdf.GetStringWidth(s) + 4
}

func scoreLabel(sev string, score *float64) string {
	if score == nil {
		return sev
	}
	return fmt.Sprintf("%s %.1f", sev, *score)
}

func cveOf(v ReportVuln) string {
	for _, a := range v.Aliases {
		if strings.HasPrefix(a, "CVE-") && a != v.ID {
			return a
		}
	}
	return ""
}

// truncate shortens s to fit width w, with an ellipsis.
func truncate(pdf *fpdf.Fpdf, s string, w float64) string {
	if pdf.GetStringWidth(s) <= w {
		return s
	}
	rs := []rune(s)
	for len(rs) > 1 && pdf.GetStringWidth(string(rs)+"…") > w {
		rs = rs[:len(rs)-1]
	}
	return string(rs) + "…"
}

// pdfSafe keeps text within what the embedded fonts can draw. Data in a
// report is Latin — package names, versions, advisory summaries — and the
// fonts keep Latin whole; anything else would print as an empty box, so it
// is shown as "?" instead, which at least says something was there.
func pdfSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return ' '
		case r >= 0x20 && r <= 0x7E, r >= 0xA0 && r <= 0x24F, r >= 0x2000 && r <= 0x206F,
			r == 0x20AC, r == 0x2122, r >= 0x2190 && r <= 0x21FF:
			return r
		case r < 0x20:
			return -1
		}
		return '?'
	}, s)
}
