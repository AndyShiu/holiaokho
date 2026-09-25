package vuln

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font/sfnt"
)

func sampleFindings() []Finding {
	f := func(repo, ns, name, ver, id, sev string, fixed ...string) Finding {
		fmt := "maven"
		if ns == "" {
			fmt = "npm"
		}
		return Finding{Repository: repo, Format: fmt, Namespace: ns, Name: name, Version: ver, VulnID: id,
			Aliases: []string{"CVE-" + id}, Severity: sev, Summary: "summary of " + id, FixedIn: fixed, FirstSeen: time.Unix(0, 0)}
	}
	return []Finding{
		f("maven-central", "org.apache.logging.log4j", "log4j-core", "2.14.1", "A", SeverityCritical, "2.15.0", "2.3.1", "2.12.2"),
		f("maven-central", "org.apache.logging.log4j", "log4j-core", "2.14.1", "B", SeverityCritical, "2.16.0", "2.12.2"),
		f("maven-central", "org.apache.logging.log4j", "log4j-core", "2.14.1", "C", SeverityModerate, "2.17.1"),
		// the same package in a second repository is the same finding
		f("maven-mirror", "org.apache.logging.log4j", "log4j-core", "2.14.1", "A", SeverityCritical, "2.15.0", "2.3.1", "2.12.2"),
		f("npm-proxy", "", "lodash", "4.17.20", "D", SeverityHigh, "4.17.21"),
		f("npm-proxy", "", "lodash", "4.17.20", "E", SeverityLow), // no fix published
	}
}

func sampleReport() *Report {
	r := &Report{GeneratedAt: time.Unix(0, 0).UTC(), Version: "test", Source: "OSV.dev"}
	assemble(r, sampleFindings())
	return r
}

func TestAssembleGroupsByPackageAndPicksTheUpgrade(t *testing.T) {
	r := sampleReport()
	if len(r.Packages) != 2 {
		t.Fatalf("%d packages, want 2", len(r.Packages))
	}
	log4j, lodash := r.Packages[0], r.Packages[1]
	if log4j.Name != "org.apache.logging.log4j:log4j-core" || len(log4j.Vulnerabilities) != 3 {
		t.Fatalf("log4j: %+v", log4j)
	}
	if strings.Join(log4j.Repositories, ",") != "maven-central,maven-mirror" {
		t.Fatalf("repositories: %v", log4j.Repositories)
	}
	// 2.15.0, 2.16.0 and 2.17.1 each fix one; only 2.17.1 fixes all three.
	if log4j.UpgradeTo != "2.17.1" || !log4j.FixesAll {
		t.Fatalf("upgrade: %q all=%v", log4j.UpgradeTo, log4j.FixesAll)
	}
	if log4j.Vulnerabilities[0].FixTo != "2.15.0" {
		t.Fatalf("per-vulnerability fix: %q", log4j.Vulnerabilities[0].FixTo)
	}
	if lodash.UpgradeTo != "4.17.21" || lodash.FixesAll {
		t.Fatalf("lodash: %q all=%v — one vulnerability has no fix", lodash.UpgradeTo, lodash.FixesAll)
	}
	if lodash.PURL != "pkg:npm/lodash@4.17.20" {
		t.Fatalf("purl %q", lodash.PURL)
	}
	if r.Summary.Vulnerabilities != 5 || r.Summary.BySeverity[SeverityCritical] != 2 {
		t.Fatalf("summary %+v", r.Summary)
	}
}

func TestCSVIsParseableAndDefusesFormulas(t *testing.T) {
	r := sampleReport()
	r.Packages[0].Vulnerabilities[0].Summary = `=HYPERLINK("http://example.com","x")`
	var b bytes.Buffer
	if err := WriteCSV(&b, r); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&b).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rows[0], ",") != strings.Join(CSVColumns, ",") || len(rows) != 6 {
		t.Fatalf("%d rows, header %v", len(rows), rows[0])
	}
	if !strings.HasPrefix(rows[1][9], "'=") {
		t.Fatalf("formula not defused: %q", rows[1][9])
	}
}

func TestJSONRoundTrips(t *testing.T) {
	var b bytes.Buffer
	if err := WriteJSON(&b, sampleReport()); err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(b.Bytes(), &back); err != nil || len(back.Packages) != 2 || back.Packages[0].UpgradeTo != "2.17.1" {
		t.Fatalf("%v %+v", err, back)
	}
}

func TestXLSXIsAWellFormedWorkbook(t *testing.T) {
	for _, lang := range []string{"en", "zh-TW", "ja"} {
		var b bytes.Buffer
		r := sampleReport()
		r.Packages[0].Vulnerabilities[0].Summary = "control \x01 character & <markup>"
		if err := WriteXLSX(&b, r, lang); err != nil {
			t.Fatal(err)
		}
		z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, f := range z.File {
			names[f.Name] = true
			rc, _ := f.Open()
			data, _ := io.ReadAll(rc)
			rc.Close()
			// Every part must parse; a single bad character makes Excel
			// reject the whole file.
			d := xml.NewDecoder(bytes.NewReader(data))
			for {
				if _, err := d.Token(); err == io.EOF {
					break
				} else if err != nil {
					t.Fatalf("%s %s: %v", lang, f.Name, err)
				}
			}
		}
		for _, want := range []string{"[Content_Types].xml", "xl/workbook.xml", "xl/worksheets/sheet3.xml"} {
			if !names[want] {
				t.Fatalf("%s: missing %s", lang, want)
			}
		}
	}
}

func TestPDFIsWrittenInEveryLanguage(t *testing.T) {
	for _, lang := range []string{"en", "zh-TW", "zh-CN", "ja", "ko"} {
		var b bytes.Buffer
		if err := WritePDF(&b, sampleReport(), lang); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		if !bytes.HasPrefix(b.Bytes(), []byte("%PDF-")) || b.Len() < 5000 {
			t.Fatalf("%s: not a PDF (%d bytes)", lang, b.Len())
		}
	}
	var b bytes.Buffer
	if err := WritePDF(&b, &Report{Version: "test"}, "zh-TW"); err != nil { // nothing found
		t.Fatal(err)
	}
}

// Every character of every label must be in the embedded font for its
// language, or the PDF prints an empty box. Fails when a label changes
// without running scripts/pdf-fonts.sh.
func TestPDFFontsCoverLabels(t *testing.T) {
	for lang := range labels {
		family := fontFor(lang)
		for _, style := range []string{"regular", "bold"} {
			data, err := pdfFonts.ReadFile("fonts/" + family + "-" + style + ".ttf")
			if err != nil {
				t.Fatal(err)
			}
			f, err := sfnt.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			var buf sfnt.Buffer
			for key, text := range labels[lang] {
				for _, r := range text {
					if r == ' ' {
						continue
					}
					if gi, err := f.GlyphIndex(&buf, r); err != nil || gi == 0 {
						t.Errorf("%s %s: %q in label %q is missing — run scripts/pdf-fonts.sh", family, style, r, key)
					}
				}
			}
		}
	}
}
