package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

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
	totalFound := 0
	totalHit := 0

	for _, p := range sortedProfiles {
		if len(p.SourceFile) > maxFileLen {
			maxFileLen = len(p.SourceFile)
		}
		totalFound += p.LinesFound
		totalHit += p.LinesHit
	}

	totalCov := 0.0
	if totalFound > 0 {
		totalCov = float64(totalHit) / float64(totalFound) * 100.0
	}

	sep := fmt.Sprintf("%s-+-------+---------+---------", strings.Repeat("-", maxFileLen))

	// Header
	if _, err := fmt.Fprintf(w, "\n%-*s | Lines | Covered | Coverage\n", maxFileLen, "File"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, sep); err != nil {
		return err
	}

	// Rows
	for _, p := range sortedProfiles {
		if _, err := fmt.Fprintf(w, "%-*s | %5d | %7d | %7.1f%%\n",
			maxFileLen, p.SourceFile, p.LinesFound, p.LinesHit, p.Coverage()); err != nil {
			return err
		}
	}

	// Footer total
	if len(sortedProfiles) > 1 {
		if _, err := fmt.Fprintln(w, sep); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%-*s | %5d | %7d | %7.1f%%\n",
			maxFileLen, "Total", totalFound, totalHit, totalCov); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(w)
	return err
}
