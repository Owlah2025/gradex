package catalog

import (
	"context"
	"errors"
	"testing"
)

func TestPricingValidationRules(t *testing.T) {
	ctx := context.Background()
	r := &Repository{}

	t.Run("SetCoursePrice input validations", func(t *testing.T) {
		tests := []struct {
			name    string
			req     SetCoursePriceRequest
			wantErr error
			errMsg  string
		}{
			{
				name: "missing course ID",
				req: SetCoursePriceRequest{
					CourseID:        "",
					AdminAccountID:  "admin-1",
					ActorDescriptor: "admin-1",
					PriceMinorUnits: 1000,
					Reason:          "Initial price",
				},
				wantErr: ErrCourseNotFound,
			},
			{
				name: "missing admin account ID",
				req: SetCoursePriceRequest{
					CourseID:        "course-1",
					AdminAccountID:  "",
					ActorDescriptor: "admin-1",
					PriceMinorUnits: 1000,
					Reason:          "Initial price",
				},
				errMsg: "admin account ID is required",
			},
			{
				name: "negative price",
				req: SetCoursePriceRequest{
					CourseID:        "course-1",
					AdminAccountID:  "admin-1",
					ActorDescriptor: "admin-1",
					PriceMinorUnits: -500,
					Reason:          "Initial price",
				},
				wantErr: ErrInvalidPrice,
			},
			{
				name: "blank reason",
				req: SetCoursePriceRequest{
					CourseID:        "course-1",
					AdminAccountID:  "admin-1",
					ActorDescriptor: "admin-1",
					PriceMinorUnits: 1000,
					Reason:          "   ",
				},
				wantErr: ErrReasonRequired,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := r.SetCoursePrice(ctx, tt.req)
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Errorf("got err %v, want %v", err, tt.wantErr)
				}
				if tt.errMsg != "" && (err == nil || err.Error() != tt.errMsg) {
					t.Errorf("got err %v, want msg %q", err, tt.errMsg)
				}
			})
		}
	})

	t.Run("SetSectionPrice input validations", func(t *testing.T) {
		tests := []struct {
			name    string
			req     SetSectionPriceRequest
			wantErr error
			errMsg  string
		}{
			{
				name: "missing course ID",
				req: SetSectionPriceRequest{
					CourseID:          "",
					SectionIdentityID: "sec-1",
					AdminAccountID:    "admin-1",
					ActorDescriptor:   "admin-1",
					PriceMinorUnits:   500,
					Reason:            "Section discount",
				},
				wantErr: ErrCourseNotFound,
			},
			{
				name: "missing section identity ID",
				req: SetSectionPriceRequest{
					CourseID:          "course-1",
					SectionIdentityID: "",
					AdminAccountID:    "admin-1",
					ActorDescriptor:   "admin-1",
					PriceMinorUnits:   500,
					Reason:            "Section discount",
				},
				wantErr: ErrCourseNotFound,
			},
			{
				name: "missing admin account ID",
				req: SetSectionPriceRequest{
					CourseID:          "course-1",
					SectionIdentityID: "sec-1",
					AdminAccountID:    "",
					ActorDescriptor:   "admin-1",
					PriceMinorUnits:   500,
					Reason:            "Section discount",
				},
				errMsg: "admin account ID is required",
			},
			{
				name: "negative price",
				req: SetSectionPriceRequest{
					CourseID:          "course-1",
					SectionIdentityID: "sec-1",
					AdminAccountID:    "admin-1",
					ActorDescriptor:   "admin-1",
					PriceMinorUnits:   -100,
					Reason:            "Section discount",
				},
				wantErr: ErrInvalidPrice,
			},
			{
				name: "blank reason",
				req: SetSectionPriceRequest{
					CourseID:          "course-1",
					SectionIdentityID: "sec-1",
					AdminAccountID:    "admin-1",
					ActorDescriptor:   "admin-1",
					PriceMinorUnits:   500,
					Reason:            "",
				},
				wantErr: ErrReasonRequired,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := r.SetSectionPrice(ctx, tt.req)
				if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
					t.Errorf("got err %v, want %v", err, tt.wantErr)
				}
				if tt.errMsg != "" && (err == nil || err.Error() != tt.errMsg) {
					t.Errorf("got err %v, want msg %q", err, tt.errMsg)
				}
			})
		}
	})

	t.Run("GetCoursePriceHistory validates courseID", func(t *testing.T) {
		_, err := r.GetCoursePriceHistory(ctx, "")
		if !errors.Is(err, ErrCourseNotFound) {
			t.Errorf("got err %v, want ErrCourseNotFound", err)
		}
	})
}
