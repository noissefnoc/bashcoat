package report

import (
	"encoding/xml"
	"io"
	"path"
	"path/filepath"
	"sort"
	"time"

	"github.com/noissefnoc/bashcoat/internal/trace"
)

const coberturaDoctype = `<!DOCTYPE coverage SYSTEM "http://cobertura.sourceforge.net/xml/coverage-04.dtd">` + "\n"

// CoberturaOptions customizes metadata written to Cobertura XML reports.
type CoberturaOptions struct {
	SourceDir string    // Source root path written to <sources><source>; defaults to "." when empty.
	Version   string    // Generator version written to <coverage version>; defaults to "bashcoat" when empty.
	Timestamp time.Time // Report generation timestamp; defaults to time.Now() when zero.
}

type coberturaCoverage struct {
	XMLName         xml.Name          `xml:"coverage"`
	LineRate        float64           `xml:"line-rate,attr"`
	BranchRate      float64           `xml:"branch-rate,attr"`
	LinesCovered    int               `xml:"lines-covered,attr"`
	LinesValid      int               `xml:"lines-valid,attr"`
	BranchesCovered int               `xml:"branches-covered,attr"`
	BranchesValid   int               `xml:"branches-valid,attr"`
	Complexity      float64           `xml:"complexity,attr"`
	Version         string            `xml:"version,attr"`
	Timestamp       int64             `xml:"timestamp,attr"`
	Sources         coberturaSources  `xml:"sources"`
	Packages        coberturaPackages `xml:"packages"`
}

type coberturaSources struct {
	Source []string `xml:"source"`
}

type coberturaPackages struct {
	Packages []coberturaPackage `xml:"package"`
}

type coberturaPackage struct {
	Name       string           `xml:"name,attr"`
	LineRate   float64          `xml:"line-rate,attr"`
	BranchRate float64          `xml:"branch-rate,attr"`
	Complexity float64          `xml:"complexity,attr"`
	Classes    coberturaClasses `xml:"classes"`
}

type coberturaClasses struct {
	Classes []coberturaClass `xml:"class"`
}

type coberturaClass struct {
	Name       string           `xml:"name,attr"`
	Filename   string           `xml:"filename,attr"`
	LineRate   float64          `xml:"line-rate,attr"`
	BranchRate float64          `xml:"branch-rate,attr"`
	Complexity float64          `xml:"complexity,attr"`
	Methods    coberturaMethods `xml:"methods"`
	Lines      coberturaLines   `xml:"lines"`
}

type coberturaMethods struct{}

type coberturaLines struct {
	Lines []coberturaLine `xml:"line"`
}

type coberturaLine struct {
	Number int  `xml:"number,attr"`
	Hits   int  `xml:"hits,attr"`
	Branch bool `xml:"branch,attr"`
}

// WriteCobertura formats profiles into Cobertura XML (coverage-04.dtd) using default options.
func WriteCobertura(w io.Writer, profiles []*trace.Profile) error {
	return WriteCoberturaWithOptions(w, profiles, CoberturaOptions{})
}

// WriteCoberturaWithOptions formats profiles into Cobertura XML (coverage-04.dtd).
func WriteCoberturaWithOptions(w io.Writer, profiles []*trace.Profile, opts CoberturaOptions) error {
	sourceDir := opts.SourceDir
	if sourceDir == "" {
		sourceDir = "."
	} else {
		sourceDir = filepath.ToSlash(sourceDir)
	}

	version := opts.Version
	if version == "" {
		version = "bashcoat"
	}

	ts := opts.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	pkgGroups := make(map[string][]*trace.Profile)
	for _, p := range profiles {
		slashPath := filepath.ToSlash(p.SourceFile)
		pkgName := path.Dir(slashPath)
		pkgGroups[pkgName] = append(pkgGroups[pkgName], p)
	}

	pkgNames := make([]string, 0, len(pkgGroups))
	for name := range pkgGroups {
		pkgNames = append(pkgNames, name)
	}
	sort.Strings(pkgNames)

	totalValid := 0
	totalCovered := 0
	packages := make([]coberturaPackage, 0, len(pkgNames))

	for _, pkgName := range pkgNames {
		group := make([]*trace.Profile, len(pkgGroups[pkgName]))
		copy(group, pkgGroups[pkgName])
		sort.Slice(group, func(i, j int) bool {
			return filepath.ToSlash(group[i].SourceFile) < filepath.ToSlash(group[j].SourceFile)
		})

		pkgValid := 0
		pkgCovered := 0
		classes := make([]coberturaClass, 0, len(group))

		for _, p := range group {
			slashPath := filepath.ToSlash(p.SourceFile)

			lineNums := make([]int, 0, len(p.Hits))
			for line := range p.Hits {
				lineNums = append(lineNums, line)
			}
			sort.Ints(lineNums)

			lines := make([]coberturaLine, 0, len(lineNums))
			for _, line := range lineNums {
				lines = append(lines, coberturaLine{
					Number: line,
					Hits:   p.Hits[line],
					Branch: false,
				})
			}

			pkgValid += p.LinesFound
			pkgCovered += p.LinesHit

			classes = append(classes, coberturaClass{
				Name:       path.Base(slashPath),
				Filename:   slashPath,
				LineRate:   calcLineRate(p.LinesHit, p.LinesFound),
				BranchRate: 0,
				Complexity: 0,
				Lines:      coberturaLines{Lines: lines},
			})
		}

		totalValid += pkgValid
		totalCovered += pkgCovered

		packages = append(packages, coberturaPackage{
			Name:       pkgName,
			LineRate:   calcLineRate(pkgCovered, pkgValid),
			BranchRate: 0,
			Complexity: 0,
			Classes:    coberturaClasses{Classes: classes},
		})
	}

	doc := coberturaCoverage{
		LineRate:        calcLineRate(totalCovered, totalValid),
		BranchRate:      0,
		LinesCovered:    totalCovered,
		LinesValid:      totalValid,
		BranchesCovered: 0,
		BranchesValid:   0,
		Complexity:      0,
		Version:         version,
		Timestamp:       ts.Unix(),
		Sources:         coberturaSources{Source: []string{sourceDir}},
		Packages:        coberturaPackages{Packages: packages},
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	if _, err := io.WriteString(w, coberturaDoctype); err != nil {
		return err
	}

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func calcLineRate(hit, found int) float64 {
	if found <= 0 {
		return 0
	}
	return float64(hit) / float64(found)
}
