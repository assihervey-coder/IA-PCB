package exportapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
)

// utf8BOM is prepended to the CSV so spreadsheet applications detect the
// encoding.
const utf8BOM = "\xEF\xBB\xBF"

// BOMLine is one grouped bill-of-materials line.
type BOMLine struct {
	Refs      []string
	Qty       int
	Value     string
	Footprint string
}

// BOMService exports the bill of materials of a project as CSV.
type BOMService struct {
	projects domainproject.Repository
	log      *slog.Logger
}

// NewBOMService builds the BOM export use case.
func NewBOMService(projects domainproject.Repository, log *slog.Logger) *BOMService {
	return &BOMService{projects: projects, log: log}
}

// ExportCSV groups the components by (value, footprint) and renders the CSV
// with the header `Ref;Qty;Value;Footprint` (semicolon separator, UTF-8).
func (s *BOMService) ExportCSV(ctx context.Context, projectID string) ([]byte, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("export bom : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("export bom : chargement du projet : %w", err)
	}

	type groupKey struct{ value, footprint string }
	groups := map[groupKey]*BOMLine{}
	var order []groupKey

	addComponent := func(ref, value, footprint string) {
		key := groupKey{value: value, footprint: footprint}
		g, ok := groups[key]
		if !ok {
			g = &BOMLine{Value: value, Footprint: footprint}
			groups[key] = g
			order = append(order, key)
		}
		g.Refs = append(g.Refs, ref)
		g.Qty++
	}

	if sch := p.Schematic(); sch != nil && len(sch.Components) > 0 {
		for _, c := range sch.Components {
			addComponent(c.Ref, c.Value, c.Footprint)
		}
	} else if b := p.Board(); b != nil {
		for i := range b.Components {
			bc := &b.Components[i]
			addComponent(bc.Ref, bc.Footprint.Value, bc.Footprint.Name)
		}
	}

	// Ordre déterministe : valeur puis empreinte, références triées
	// naturellement (R1, R2, R10).
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].value != order[j].value {
			return order[i].value < order[j].value
		}
		return order[i].footprint < order[j].footprint
	})

	var sb strings.Builder
	sb.WriteString(utf8BOM)
	sb.WriteString("Ref;Qty;Value;Footprint\n")
	for _, key := range order {
		g := groups[key]
		refs := append([]string(nil), g.Refs...)
		sort.Slice(refs, func(a, b int) bool { return naturalLess(refs[a], refs[b]) })
		sb.WriteString(csvField(strings.Join(refs, ",")))
		sb.WriteByte(';')
		sb.WriteString(strconv.Itoa(g.Qty))
		sb.WriteByte(';')
		sb.WriteString(csvField(g.Value))
		sb.WriteByte(';')
		sb.WriteString(csvField(g.Footprint))
		sb.WriteByte('\n')
	}

	s.log.Info("export bom", "project_id", projectID, "lines", len(order))
	return []byte(sb.String()), nil
}

// csvField quotes a field when it contains the separator, quotes or newlines.
func csvField(v string) string {
	if strings.ContainsAny(v, ";\"\n\r") {
		return "\"" + strings.ReplaceAll(v, "\"", "\"\"") + "\""
	}
	return v
}

// naturalLess compares two reference designators so that R2 < R10.
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		da, db := isDigit(ca), isDigit(cb)
		switch {
		case da && db:
			// Compare the two numeric runs by value.
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na, _ := strconv.ParseInt(a[si:i], 10, 64)
			nb, _ := strconv.ParseInt(b[sj:j], 10, 64)
			if na != nb {
				return na < nb
			}
		default:
			if ca != cb {
				return ca < cb
			}
			i++
			j++
		}
	}
	return len(a)-i < len(b)-j
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
