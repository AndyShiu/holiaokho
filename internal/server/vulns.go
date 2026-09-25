package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/holiaokho/holiaokho/internal/notify"
	"github.com/holiaokho/holiaokho/internal/vuln"
)

// mailLimit caps how many findings one email lists. The first scan of an
// established instance can turn up hundreds; the mail says how many there
// are and points at the page that lists them all.
const mailLimit = 50

// announceVulnerabilities sends newly found vulnerabilities as one email to
// the notification recipients and one "vulnerability.found" webhook event.
// Both are best effort: an instance with neither configured is normal.
func (s *Server) announceVulnerabilities(ctx context.Context, found []vuln.Finding) error {
	counts := map[string]int{}
	for _, f := range found {
		counts[f.Severity]++
	}
	var parts []string
	for _, sev := range []string{vuln.SeverityCritical, vuln.SeverityHigh, vuln.SeverityModerate, vuln.SeverityLow, vuln.SeverityUnknown} {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.ToLower(sev)))
		}
	}
	headline := fmt.Sprintf("%d new known vulnerabilities in stored packages (%s)", len(found), strings.Join(parts, ", "))

	events := found
	if len(events) > 100 {
		events = events[:100]
	}
	s.Notify.Emit(notify.Event{Event: "vulnerability.found", Data: map[string]any{
		"count":    len(found),
		"counts":   counts,
		"findings": events,
	}})

	var b strings.Builder
	b.WriteString(headline + ".\n\n")
	for i, f := range found {
		if i == mailLimit {
			fmt.Fprintf(&b, "\n…and %d more.\n", len(found)-mailLimit)
			break
		}
		pkg := f.Name
		if f.Namespace != "" {
			pkg = f.Namespace + "/" + f.Name
			if f.Format == "maven" {
				pkg = f.Namespace + ":" + f.Name
			}
		}
		fmt.Fprintf(&b, "%-8s %s  %s %s  (%s)\n", f.Severity, f.VulnID, pkg, f.Version, f.Repository)
		if f.Summary != "" {
			fmt.Fprintf(&b, "         %s\n", f.Summary)
		}
		if len(f.FixedIn) > 0 {
			fmt.Fprintf(&b, "         fixed in %s\n", strings.Join(f.FixedIn, ", "))
		}
		fmt.Fprintf(&b, "         https://osv.dev/vulnerability/%s\n", f.VulnID)
	}
	if base := strings.TrimSuffix(s.Cfg.Server.BaseURL, "/"); base != "" {
		fmt.Fprintf(&b, "\nAll findings: %s/ui/vulnerabilities\n", base)
	}
	if err := s.Notify.SendMail(ctx, nil, "[Holiaokho] "+headline, b.String()); err != nil {
		s.Log.Debug("vulnerability mail not sent", "err", err)
	}
	return nil
}
