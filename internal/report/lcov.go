// Package report formats collected coverage profiles into LCOV and human-readable summary tables.
package report

import (
	"fmt"
	"io"
	"sort"

	"github.com/noissefnoc/bashcoat/internal/trace"
)

// WriteLCOV formats profiles into standard LCOV (lcov.info) format.
// This format is natively supported by Codecov, Coveralls, SonarQube, and VS Code.
func WriteLCOV(w io.Writer, profiles []*trace.Profile) error {
	// Sort profiles by filename for deterministic output
	sortedProfiles := make([]*trace.Profile, len(profiles))
	copy(sortedProfiles, profiles)
	sort.Slice(sortedProfiles, func(i, j int) bool {
		return sortedProfiles[i].SourceFile < sortedProfiles[j].SourceFile
	})

	for _, p := range sortedProfiles {
		if _, err := fmt.Fprintf(w, "TN:\nSF:%s\n", p.SourceFile); err != nil {
			return err
		}

		// Sort line numbers ascending
		lines := make([]int, 0, len(p.Hits))
		for line := range p.Hits {
			lines = append(lines, line)
		}
		sort.Ints(lines)

		for _, line := range lines {
			if _, err := fmt.Fprintf(w, "DA:%d,%d\n", line, p.Hits[line]); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintf(w, "LF:%d\nLH:%d\nend_of_record\n", p.LinesFound, p.LinesHit); err != nil {
			return err
		}
	}

	return nil
}
