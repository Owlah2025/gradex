package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Owlah2025/gradex/backend/internal/academic"
	"github.com/Owlah2025/gradex/backend/internal/problem"
)

type subjectRequestHandlers struct{ repo *academic.Repository }

type createSubjectRequestBody struct {
	InstitutionID        string  `json:"institution_id"`
	CourseID             *string `json:"course_id"`
	ProposedOfficialCode *string `json:"proposed_official_code"`
	ProposedTitleAr      string  `json:"proposed_title_ar"`
	ProposedTitleEn      string  `json:"proposed_title_en"`
	AcademicContext      *string `json:"academic_context"`
	Note                 *string `json:"note"`
}

func (h *subjectRequestHandlers) listOwn(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	var courseID *string
	if value := strings.TrimSpace(c.Query("course_id")); value != "" {
		courseID = &value
	}
	requests, err := h.repo.ListSubjectRequests(c.Request.Context(), academic.ListSubjectRequestsRequest{
		RequesterAccountID: accountID, CourseID: courseID,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, requests)
}

func (h *subjectRequestHandlers) create(c *gin.Context) {
	accountID := c.GetString(ctxUserIDKey)
	var body createSubjectRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	request, err := h.repo.CreateSubjectRequest(c.Request.Context(), academic.CreateSubjectRequestWorkflow{
		RequesterAccountID: accountID, ActorDescriptor: accountID,
		InstitutionID: strings.TrimSpace(body.InstitutionID), CourseID: body.CourseID,
		ProposedOfficialCode: body.ProposedOfficialCode,
		ProposedTitleAr:      body.ProposedTitleAr, ProposedTitleEn: body.ProposedTitleEn,
		AcademicContext: body.AcademicContext, Note: body.Note,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusCreated, request)
}

func (h *subjectRequestHandlers) listAdmin(c *gin.Context) {
	var status *academic.SubjectRequestStatus
	if value := strings.TrimSpace(c.Query("status")); value != "" {
		parsed := academic.SubjectRequestStatus(value)
		status = &parsed
	}
	requests, err := h.repo.ListSubjectRequests(c.Request.Context(), academic.ListSubjectRequestsRequest{Status: status})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, requests)
}

func subjectRequestAdminActor(c *gin.Context) academic.Actor {
	id := c.GetString(ctxUserIDKey)
	return academic.Actor{AdminAccountID: id, ActorDescriptor: id}
}

type linkSubjectRequestBody struct {
	SubjectID string `json:"subject_id"`
}

func (h *subjectRequestHandlers) link(c *gin.Context) {
	var body linkSubjectRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	request, err := h.repo.LinkSubjectRequest(c.Request.Context(), academic.LinkSubjectRequest{
		Actor: subjectRequestAdminActor(c), RequestID: c.Param("requestId"),
		SubjectID: strings.TrimSpace(body.SubjectID),
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, request)
}

func (h *subjectRequestHandlers) approveNew(c *gin.Context) {
	request, err := h.repo.ApproveSubjectRequestAsNew(c.Request.Context(), academic.ApproveSubjectRequestAsNew{
		Actor: subjectRequestAdminActor(c), RequestID: c.Param("requestId"),
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, request)
}

type rejectSubjectRequestBody struct {
	Reason string `json:"reason"`
}

func (h *subjectRequestHandlers) reject(c *gin.Context) {
	var body rejectSubjectRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		writeProblem(c, problem.Malformed())
		return
	}
	request, err := h.repo.RejectSubjectRequest(c.Request.Context(), academic.RejectSubjectRequest{
		Actor: subjectRequestAdminActor(c), RequestID: c.Param("requestId"), Reason: body.Reason,
	})
	if err != nil {
		writeAcademicError(c, err)
		return
	}
	c.JSON(http.StatusOK, request)
}
