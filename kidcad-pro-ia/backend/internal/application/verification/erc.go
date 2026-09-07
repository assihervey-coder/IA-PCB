package verificationapp

import (
	"context"
	"errors"
	"fmt"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
)

// ERC violation codes (produced by the domain schematic validation).
const (
	CodeERCDuplicateRef         = "ERC_DUPLICATE_REF"
	CodeERCSinglePinNet         = "ERC_SINGLE_PIN_NET"
	CodeERCEmptyNetName         = "ERC_EMPTY_NET_NAME"
	CodeERCMissingFootprint     = "ERC_MISSING_FOOTPRINT"
	CodeERCUnconnectedComponent = "ERC_UNCONNECTED_COMPONENT"
	CodeERCKnownComponent       = "ERC_UNKNOWN_COMPONENT"
)

// ERCViolation is one electrical-rule violation.
type ERCViolation struct {
	Code         string `json:"code"`
	Severity     string `json:"severity"`
	Message      string `json:"message"`
	ComponentRef string `json:"component_ref"`
}

// ERCResult is the outcome of an electrical-rule check.
type ERCResult struct {
	Passed     bool           `json:"passed"`
	Violations []ERCViolation `json:"violations"`
}

// ERCChecker runs the electrical-rule check of a project schematic. It
// delegates the actual analysis to the domain aggregate (Schematic.Validate)
// and maps the issues onto the REST-facing payload.
type ERCChecker struct {
	projects domainproject.Repository
}

// NewERCChecker builds the ERC use case.
func NewERCChecker(projects domainproject.Repository) *ERCChecker {
	return &ERCChecker{projects: projects}
}

// Run executes the schematic validation. A project without a schematic
// passes trivially (nothing to check).
func (c *ERCChecker) Run(ctx context.Context, projectID string) (*ERCResult, error) {
	p, err := c.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("erc : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("erc : chargement du projet : %w", err)
	}

	res := &ERCResult{Passed: true, Violations: []ERCViolation{}}
	sch := p.Schematic()
	if sch == nil {
		return res, nil
	}

	for _, issue := range sch.Validate() {
		severity := string(issue.Severity)
		if severity != string(domainschematic.SeverityError) {
			// warning et info sont remontés comme avertissements côté API.
			severity = "warning"
		}
		res.Violations = append(res.Violations, ERCViolation{
			Code:         issue.Code,
			Severity:     severity,
			Message:      issue.Message,
			ComponentRef: issue.Ref,
		})
		if severity == string(domainschematic.SeverityError) {
			res.Passed = false
		}
	}
	return res, nil
}
