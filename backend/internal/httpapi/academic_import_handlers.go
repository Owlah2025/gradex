package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/academic"
	"github.com/Owlah2025/gradex/backend/internal/academic/importer"
	"github.com/Owlah2025/gradex/backend/internal/academic/manifest"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

// Admin catalog import.
//
// The only caller-supplied selector is a manifest identifier that must match a
// manifest compiled into this binary. There is deliberately no filesystem path,
// no URL, and no uploaded file: a client cannot make the server read data that
// was not reviewed and checked in. Browser CSV upload is post-launch scope and
// is not implemented.
type academicImportHandlers struct {
	repo *academic.Repository
}

type importBody struct {
	Manifest string `json:"manifest"`
	Mode     string `json:"mode"`
}

// listManifests exposes only the identifiers an Admin may select, so the Admin
// surface never has to know a path.
func (h *academicImportHandlers) listManifests(c *gin.Context) {
	ids, err := manifest.Available()
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	summaries := make([]gin.H, 0, len(ids))
	for _, id := range ids {
		pkg, err := manifest.Load(id)
		if err != nil {
			writeProblem(c, problem.Internal(""))
			return
		}
		summaries = append(summaries, gin.H{
			"manifest":         pkg.Manifest.ID,
			"version":          pkg.Manifest.Version,
			"description":      strings.TrimSpace(pkg.Manifest.Description),
			"institution_slug": pkg.Manifest.Institution.Slug,
			"institution_name": pkg.Manifest.Institution.NameEn,
			"counts": gin.H{
				"academic_units":      len(pkg.Manifest.Units),
				"programs":            len(pkg.Manifest.Programs),
				"curricula":           len(pkg.Manifest.Curricula),
				"subjects":            len(pkg.Manifest.Subjects),
				"curriculum_subjects": len(pkg.Manifest.Mappings),
				"sources":             len(pkg.Sources.Sources),
			},
		})
	}
	c.JSON(http.StatusOK, summaries)
}

func (h *academicImportHandlers) runImport(c *gin.Context) {
	var body importBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	selected := strings.TrimSpace(body.Manifest)
	if selected == "" {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "MANIFEST_REQUIRED", Location: problem.LocationBody,
			Detail: "a known manifest identifier is required",
		}))
		return
	}
	// Reject anything that looks like a path or a URL before touching the
	// registry, so the refusal is unambiguous in logs and in tests.
	if strings.ContainsAny(selected, "/\\:.") {
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "MANIFEST_NOT_AN_IDENTIFIER", Location: problem.LocationBody,
			Detail: "the manifest must be a known identifier, not a path or URL",
		}))
		return
	}

	var apply bool
	switch strings.ToLower(strings.TrimSpace(body.Mode)) {
	case "dry_run", "dry-run", "":
		apply = false
	case "apply":
		apply = true
	default:
		writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
			Code: "UNSUPPORTED_IMPORT_MODE", Location: problem.LocationBody,
			Detail: `mode must be "dry_run" or "apply"`,
		}))
		return
	}

	pkg, err := manifest.Load(selected)
	if err != nil {
		if errors.Is(err, manifest.ErrInvalidManifest) {
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code: "MANIFEST_INVALID", Location: problem.LocationBody, Detail: err.Error(),
			}))
			return
		}
		writeProblem(c, problem.NotFound())
		return
	}

	// The institution in the path must be the institution the manifest declares.
	// Importing Kuwait University's catalog "into" another institution would be
	// a silent cross-institution write.
	institutionID := strings.TrimSpace(c.Param("institutionId"))
	if institutionID != "" {
		existing, err := h.repo.GetInstitution(c.Request.Context(), institutionID)
		switch {
		case errors.Is(err, academic.ErrNotFound):
			// An unseeded institution is the normal first-import case: the
			// manifest creates it. Nothing to cross-check yet.
		case err != nil:
			writeAcademicError(c, err)
			return
		case existing.Slug != pkg.Manifest.Institution.Slug:
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code: "MANIFEST_INSTITUTION_MISMATCH", Location: problem.LocationBody,
				Detail: "the manifest describes a different institution than the one addressed",
			}))
			return
		}
	}

	catalogImporter, err := importer.New(h.repo)
	if err != nil {
		writeProblem(c, problem.Internal(""))
		return
	}
	accountID := c.GetString(ctxUserIDKey)
	plan, err := catalogImporter.Run(c.Request.Context(), pkg, importer.Options{
		// The authenticated Admin is preserved as the audited actor on this path;
		// only the CLI uses the SYSTEM principal.
		Actor: academic.Actor{AdminAccountID: accountID, ActorDescriptor: accountID},
		Apply: apply,
	})
	if err != nil {
		if errors.Is(err, importer.ErrIdentityRebind) {
			writeProblem(c, problem.StateConflict())
			return
		}
		if errors.Is(err, manifest.ErrInvalidManifest) {
			writeProblem(c, problem.ValidationFailed().WithViolations(problem.Violation{
				Code: "MANIFEST_INVALID", Location: problem.LocationBody, Detail: err.Error(),
			}))
			return
		}
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, plan)
}
