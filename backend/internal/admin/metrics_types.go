package admin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/identity"
)

type MetricsOverviewRequest struct {
	Principal identity.Principal
	Locale    identity.Locale
	Window    string
}

type Metric struct {
	Key           string          `json:"key"`
	Value         json.RawMessage `json:"value"`
	DefinitionKey string          `json:"definition_key"`
}

type MetricsOverviewResult struct {
	Metrics []Metric  `json:"metrics"`
	AsOf    time.Time `json:"as_of"`
}

type MetricsCoursesRequest struct {
	Principal identity.Principal
	Locale    identity.Locale
	Sort      string
	Direction string
	Page      int
	Limit     int
}

type CourseMetric struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	Instructor       string  `json:"instructor"`
	Lifecycle        string  `json:"lifecycle"`
	Enrolled         int     `json:"enrolled"`
	Started          int     `json:"started"`
	LearningActive7d int     `json:"learning_active_7d"`
	AverageProgress  float64 `json:"average_progress"`
	Completed        int     `json:"completed"`
}

type MetricsCoursesResult struct {
	Items   []CourseMetric `json:"items"`
	Total   int            `json:"total"`
	Page    int            `json:"page"`
	Limit   int            `json:"limit"`
	HasMore bool           `json:"has_more"`
}

type MetricsInstructorsRequest struct {
	Principal identity.Principal
	Sort      string
	Direction string
	Page      int
	Limit     int
}

type InstructorMetric struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	PublishedCourses         int    `json:"published_courses"`
	TotalEnrollments         int    `json:"total_enrollments"`
	LearningActiveStudents7d int    `json:"learning_active_students_7d"`
}

type MetricsInstructorsResult struct {
	Items   []InstructorMetric `json:"items"`
	Total   int                `json:"total"`
	Page    int                `json:"page"`
	Limit   int                `json:"limit"`
	HasMore bool               `json:"has_more"`
}

type InboxRequest struct {
	Principal identity.Principal
	Locale    identity.Locale
	Limit     int
}

type InboxItem struct {
	Kind       string    `json:"kind"`
	Label      string    `json:"label"`
	AgeSeconds int64     `json:"age_seconds"`
	CreatedAt  time.Time `json:"created_at"`
	Route      string    `json:"route"`
	TargetID   string    `json:"target_id"`
}

type InboxSection struct {
	Key   string      `json:"key"`
	Count int         `json:"count"`
	Items []InboxItem `json:"items"`
}

type InboxResult struct {
	Sections []InboxSection `json:"sections"`
}

type AdminMetricsService interface {
	GetMetricsOverview(ctx context.Context, request MetricsOverviewRequest) (MetricsOverviewResult, error)
	ListMetricsCourses(ctx context.Context, request MetricsCoursesRequest) (MetricsCoursesResult, error)
	ListMetricsInstructors(ctx context.Context, request MetricsInstructorsRequest) (MetricsInstructorsResult, error)
	GetInbox(ctx context.Context, request InboxRequest) (InboxResult, error)
}
