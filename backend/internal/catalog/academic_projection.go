package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type AcademicSubjectProjection struct {
	OfficialCode     *string `json:"official_code,omitempty"`
	TitleAr          string  `json:"title_ar"`
	TitleEn          string  `json:"title_en"`
	OwningUnitNameAr *string `json:"owning_unit_name_ar,omitempty"`
	OwningUnitNameEn *string `json:"owning_unit_name_en,omitempty"`
	ParentUnitNameAr *string `json:"parent_unit_name_ar,omitempty"`
	ParentUnitNameEn *string `json:"parent_unit_name_en,omitempty"`
}

type AcademicCourseProjection struct {
	InstitutionNameAr string                     `json:"institution_name_ar"`
	InstitutionNameEn string                     `json:"institution_name_en"`
	Subject           *AcademicSubjectProjection `json:"subject,omitempty"`
}

func loadCourseAcademicProjection(ctx context.Context, q audienceQueryer, course *Course) error {
	if course == nil || course.ClassificationModel != ClassificationAcademicCatalog {
		return nil
	}
	var projection AcademicCourseProjection
	var code, titleAr, titleEn, unitAr, unitEn, parentAr, parentEn *string
	err := q.QueryRow(ctx, `
		SELECT institution.name_ar, institution.name_en,
		       subject.official_code, subject.title_ar, subject.title_en,
		       owning_unit.name_ar, owning_unit.name_en,
		       parent_unit.name_ar, parent_unit.name_en
		FROM courses course
		JOIN institutions institution ON institution.id = course.institution_id
		LEFT JOIN subjects subject ON subject.id = course.subject_id
		LEFT JOIN academic_units owning_unit ON owning_unit.id = subject.owning_unit_id
		LEFT JOIN academic_units parent_unit ON parent_unit.id = owning_unit.parent_unit_id
		WHERE course.id = $1::uuid`, course.ID,
	).Scan(&projection.InstitutionNameAr, &projection.InstitutionNameEn,
		&code, &titleAr, &titleEn, &unitAr, &unitEn, &parentAr, &parentEn)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCourseNotFound
	}
	if err != nil {
		return fmt.Errorf("loading semantic academic Course context: %w", err)
	}
	if titleAr != nil && titleEn != nil {
		projection.Subject = &AcademicSubjectProjection{
			OfficialCode: code, TitleAr: *titleAr, TitleEn: *titleEn,
			OwningUnitNameAr: unitAr, OwningUnitNameEn: unitEn,
			ParentUnitNameAr: parentAr, ParentUnitNameEn: parentEn,
		}
	}
	course.AcademicContext = &projection
	return nil
}
