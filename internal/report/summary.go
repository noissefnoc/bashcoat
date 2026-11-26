package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/noissefnoc/bashcoat/internal/trace"
)

// WriteSummary formats profiles into a clean, human-readable terminal table.
func WriteSummary(w io.Writer, profiles []*trace.Profile) error {
	if len(profiles) == 0 {
		_, err := fmt.Fprintln(w, "No coverage profiles collected.")
		return err
	}

	sortedProfiles := make([]*trace.Profile, len(profiles))
	copy(sortedProfiles, profiles)
	sort.Slice(sortedProfiles, func(i, j int) bool {
		return sortedProfiles[i].SourceFile < sortedProfiles[j].SourceFile
	})

	maxFileLen := 4 // "File"
	if len(sortedProfiles) > 1 {
		maxFileLen = 5 // "Total"
	}
	maxFuncsLen := 5 // "Funcs"
	totalFound := 0
	totalHit := 0
	totalFuncsFound := 0
	totalFuncsHit := 0

	for _, p := range sortedProfiles {
		if width := displayWidth(p.SourceFile); width > maxFileLen {
			maxFileLen = width
		}
		if fStr := fmt.Sprintf("%d/%d", p.FunctionsHit, p.FunctionsFound); len(fStr) > maxFuncsLen {
			maxFuncsLen = len(fStr)
		}
		totalFound += p.LinesFound
		totalHit += p.LinesHit
		totalFuncsFound += p.FunctionsFound
		totalFuncsHit += p.FunctionsHit
	}
	if len(sortedProfiles) > 1 {
		if fStr := fmt.Sprintf("%d/%d", totalFuncsHit, totalFuncsFound); len(fStr) > maxFuncsLen {
			maxFuncsLen = len(fStr)
		}
	}

	sep := fmt.Sprintf("%s-+-------+---------+----------+-%s-+---------",
		strings.Repeat("-", maxFileLen), strings.Repeat("-", maxFuncsLen))

	// Header
	if _, err := fmt.Fprintf(w, "\n%s | Lines | Covered | Coverage | %*s |   Fn Cov\n",
		padRightDisplay("File", maxFileLen), maxFuncsLen, "Funcs"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, sep); err != nil {
		return err
	}

	// Rows
	for _, p := range sortedProfiles {
		fStr := fmt.Sprintf("%d/%d", p.FunctionsHit, p.FunctionsFound)
		if _, err := fmt.Fprintf(w, "%s | %5d | %7d | %s | %*s | %s\n",
			padRightDisplay(p.SourceFile, maxFileLen), p.LinesFound, p.LinesHit, formatCoverage(p.LinesFound, p.LinesHit),
			maxFuncsLen, fStr, formatCoverage(p.FunctionsFound, p.FunctionsHit)); err != nil {
			return err
		}
	}

	// Footer total
	if len(sortedProfiles) > 1 {
		if _, err := fmt.Fprintln(w, sep); err != nil {
			return err
		}
		fStr := fmt.Sprintf("%d/%d", totalFuncsHit, totalFuncsFound)
		if _, err := fmt.Fprintf(w, "%s | %5d | %7d | %s | %*s | %s\n",
			padRightDisplay("Total", maxFileLen), totalFound, totalHit, formatCoverage(totalFound, totalHit),
			maxFuncsLen, fStr, formatCoverage(totalFuncsFound, totalFuncsHit)); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(w)
	return err
}

func formatCoverage(found, hit int) string {
	if found == 0 {
		return fmt.Sprintf("%8s", "N/A")
	}
	pct := float64(hit) / float64(found) * 100.0
	return fmt.Sprintf("%7.1f%%", pct)
}

func padRightDisplay(s string, width int) string {
	n := displayWidth(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeDisplayWidth(r)
	}
	return w
}

func runeDisplayWidth(r rune) int {
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) ||
		(r >= 0x2E80 && r <= 0x303F) ||
		(r >= 0x3190 && r <= 0x33FF) ||
		(r >= 0xFF01 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0x1F300 && r <= 0x1FAFF) {
		return 2
	}
	return 1
}
