// Package e2ediagnostic captures a bounded, secret-free media failure snapshot.
package e2ediagnostic

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/queue"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultQueueName = "default"

type Input struct {
	RunID          string                       `json:"run_id"`
	AssetVersionID string                       `json:"asset_version_id"`
	Runtime        map[string]map[string]string `json:"runtime"`
	APILogPath     string                       `json:"api_log_path"`
	WorkerLogPath  string                       `json:"worker_log_path"`
}

type Artifact struct {
	SchemaVersion  int                          `json:"schema_version"`
	RunID          string                       `json:"run_id"`
	CapturedAt     time.Time                    `json:"captured_at"`
	Runtime        map[string]map[string]string `json:"runtime"`
	Asset          *Asset                       `json:"asset,omitempty"`
	Work           []Work                       `json:"work"`
	Scans          []Scan                       `json:"scans"`
	Validations    []Validation                 `json:"validations"`
	Processing     []Processing                 `json:"processing"`
	RenditionCount int                          `json:"rendition_count"`
	Queue          []QueueTask                  `json:"queue"`
	Logs           Logs                         `json:"logs"`
	CaptureError   string                       `json:"capture_error,omitempty"`
}

type Asset struct {
	AssetID           string     `json:"asset_id"`
	AssetVersionID    string     `json:"asset_version_id"`
	Kind              string     `json:"kind"`
	State             string     `json:"state"`
	CreatedAt         time.Time  `json:"created_at"`
	UploadID          string     `json:"upload_id"`
	UploadCreatedAt   time.Time  `json:"upload_created_at"`
	UploadCompletedAt *time.Time `json:"upload_completed_at,omitempty"`
	// Provenance names which safety path admitted these exact bytes. An asset
	// can legitimately reach READY on either, so a diagnostic that assumed a
	// scan attempt would misreport a D-088 asset as broken. The two are never
	// merged: MALWARE_SCAN means a scanner inspected the object, TRUSTED_VALIDATION
	// means it did not and only the exact-version checks were performed.
	Provenance string `json:"provenance"`
}

// Provenance values. NONE is the honest answer for an asset that has not
// earned either path yet, and BOTH would be an anomaly worth seeing.
const (
	ProvenanceNone              = "NONE"
	ProvenanceMalwareScan       = "MALWARE_SCAN"
	ProvenanceTrustedValidation = "TRUSTED_VALIDATION"
	ProvenanceScanAndValidation = "MALWARE_SCAN_AND_TRUSTED_VALIDATION"
)

// Validation is D-088 exact-version validation evidence. It is reported
// separately from Scan and never relabelled as a scan: no malware inspection
// took place on this path.
type Validation struct {
	ID          string    `json:"id"`
	WorkID      string    `json:"work_id"`
	Outcome     string    `json:"outcome"`
	Validator   string    `json:"validator"`
	Profile     string    `json:"profile"`
	ValidatedAt time.Time `json:"validated_at"`
}

type Work struct {
	EventID      string     `json:"event_id"`
	EventType    string     `json:"event_type"`
	OccurredAt   time.Time  `json:"occurred_at"`
	AvailableAt  time.Time  `json:"available_at"`
	DispatchedAt *time.Time `json:"dispatched_at,omitempty"`
}

type Scan struct {
	ID        string    `json:"id"`
	WorkID    string    `json:"work_id"`
	Outcome   string    `json:"outcome"`
	Scanner   string    `json:"scanner"`
	ScannedAt time.Time `json:"scanned_at"`
}
type Processing struct {
	ID             string    `json:"id"`
	OperationID    string    `json:"operation_id"`
	State          string    `json:"state"`
	RenditionCount int       `json:"rendition_count"`
	CompletedAt    time.Time `json:"completed_at"`
}
type QueueTask struct {
	EventID      string     `json:"event_id"`
	TaskType     string     `json:"task_type"`
	State        string     `json:"state"`
	Retried      int        `json:"retried,omitempty"`
	MaxRetry     int        `json:"max_retry,omitempty"`
	LastFailedAt *time.Time `json:"last_failed_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	Orphaned     bool       `json:"orphaned,omitempty"`
}
type HTTPLog struct {
	Timestamp  string `json:"timestamp"`
	Method     string `json:"method"`
	Route      string `json:"route"`
	Status     int    `json:"status"`
	DurationMS int    `json:"duration_ms"`
}
type WorkerLog struct {
	Timestamp  string `json:"timestamp"`
	Phase      string `json:"phase,omitempty"`
	Operation  string `json:"operation,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`
	JobType    string `json:"job_type,omitempty"`
	RetryCount int    `json:"retry_count,omitempty"`
}
type Logs struct {
	APIPath    string      `json:"api_path,omitempty"`
	WorkerPath string      `json:"worker_path,omitempty"`
	API        []HTTPLog   `json:"api"`
	Worker     []WorkerLog `json:"worker"`
}

type Collector struct {
	db        *pgxpool.Pool
	inspector *asynq.Inspector
}

func New(db *pgxpool.Pool, inspector *asynq.Inspector) (*Collector, error) {
	if db == nil || inspector == nil {
		return nil, errors.New("diagnostic dependencies are required")
	}
	return &Collector{db: db, inspector: inspector}, nil
}

func (c *Collector) Capture(ctx context.Context, input Input) Artifact {
	a := Artifact{SchemaVersion: 2, RunID: input.RunID, CapturedAt: time.Now().UTC(), Runtime: safeRuntime(input.Runtime), Work: []Work{}, Scans: []Scan{}, Validations: []Validation{}, Processing: []Processing{}, Queue: []QueueTask{}, Logs: Logs{APIPath: input.APILogPath, WorkerPath: input.WorkerLogPath, API: apiLogs(input.APILogPath), Worker: workerLogs(input.WorkerLogPath)}}
	if input.AssetVersionID == "" {
		return a
	}
	asset, err := c.asset(ctx, input.AssetVersionID)
	if err != nil {
		a.CaptureError = "asset_query_failed"
		return a
	}
	a.Asset = asset
	if asset == nil {
		return a
	}
	work, err := c.work(ctx, input.AssetVersionID)
	if err != nil {
		a.CaptureError = "work_query_failed"
		return a
	}
	a.Work = work
	if a.Scans, err = c.scans(ctx, input.AssetVersionID); err != nil {
		a.CaptureError = "scan_query_failed"
		return a
	}
	if a.Validations, err = c.validations(ctx, input.AssetVersionID); err != nil {
		a.CaptureError = "validation_query_failed"
		return a
	}
	if a.Processing, err = c.processing(ctx, input.AssetVersionID); err != nil {
		a.CaptureError = "processing_query_failed"
		return a
	}
	if err := c.db.QueryRow(ctx, `SELECT count(*) FROM video_renditions WHERE asset_version_id=$1::uuid`, input.AssetVersionID).Scan(&a.RenditionCount); err != nil {
		a.CaptureError = "rendition_query_failed"
		return a
	}
	for _, event := range work {
		if event.DispatchedAt == nil {
			a.Queue = append(a.Queue, QueueTask{EventID: event.EventID, TaskType: taskType(event.EventType), State: "NOT_ENQUEUED"})
			continue
		}
		a.Queue = append(a.Queue, c.task(event))
	}
	return a
}

func (c *Collector) asset(ctx context.Context, id string) (*Asset, error) {
	var a Asset
	var scanned, validated bool
	err := c.db.QueryRow(ctx, `SELECT ma.id::text,mav.id::text,mav.kind::text,mav.state::text,mav.created_at,ui.id::text,ui.created_at,ui.completed_at,mav.successful_scan_attempt_id IS NOT NULL,mav.successful_validation_attempt_id IS NOT NULL FROM media_asset_versions mav JOIN media_assets ma ON ma.id=mav.logical_asset_id JOIN upload_intents ui ON ui.asset_version_id=mav.id WHERE mav.id=$1::uuid`, id).Scan(&a.AssetID, &a.AssetVersionID, &a.Kind, &a.State, &a.CreatedAt, &a.UploadID, &a.UploadCreatedAt, &a.UploadCompletedAt, &scanned, &validated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	a.Provenance = provenance(scanned, validated)
	return &a, err
}

func provenance(scanned, validated bool) string {
	switch {
	case scanned && validated:
		return ProvenanceScanAndValidation
	case scanned:
		return ProvenanceMalwareScan
	case validated:
		return ProvenanceTrustedValidation
	default:
		return ProvenanceNone
	}
}
func (c *Collector) work(ctx context.Context, id string) ([]Work, error) {
	rows, err := c.db.Query(ctx, `SELECT e.id::text,e.event_type,e.occurred_at,e.available_at,d.dispatched_at FROM outbox_events e LEFT JOIN media_outbox_dispatches d ON d.event_id=e.id WHERE e.source_module='MEDIA_AND_ASSETS' AND e.aggregate_type='MEDIA_ASSET_VERSION' AND e.aggregate_id=$1::uuid AND e.event_type IN ('media.scan_requested','media.transcode_requested') ORDER BY e.occurred_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Work{}
	for rows.Next() {
		var w Work
		if err := rows.Scan(&w.EventID, &w.EventType, &w.OccurredAt, &w.AvailableAt, &w.DispatchedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
func (c *Collector) scans(ctx context.Context, id string) ([]Scan, error) {
	rows, err := c.db.Query(ctx, `SELECT id::text,work_id,outcome::text,scanner_identity,scanned_at FROM scan_attempts WHERE asset_version_id=$1::uuid ORDER BY scanned_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Scan{}
	for rows.Next() {
		var v Scan
		if err := rows.Scan(&v.ID, &v.WorkID, &v.Outcome, &v.Scanner, &v.ScannedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (c *Collector) validations(ctx context.Context, id string) ([]Validation, error) {
	rows, err := c.db.Query(ctx, `SELECT id::text,work_id,outcome::text,validator_identity,profile,validated_at FROM validation_attempts WHERE asset_version_id=$1::uuid ORDER BY validated_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Validation{}
	for rows.Next() {
		var v Validation
		if err := rows.Scan(&v.ID, &v.WorkID, &v.Outcome, &v.Validator, &v.Profile, &v.ValidatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (c *Collector) processing(ctx context.Context, id string) ([]Processing, error) {
	rows, err := c.db.Query(ctx, `SELECT id::text,operation_id,state::text,rendition_count,completed_at FROM processing_attempts WHERE asset_version_id=$1::uuid ORDER BY completed_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Processing{}
	for rows.Next() {
		var v Processing
		if err := rows.Scan(&v.ID, &v.OperationID, &v.State, &v.RenditionCount, &v.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (c *Collector) task(w Work) QueueTask {
	result := QueueTask{EventID: w.EventID, TaskType: taskType(w.EventType), State: "ABSENT_AFTER_DISPATCH"}
	info, err := c.inspector.GetTaskInfo(DefaultQueueName, w.EventID)
	if err != nil {
		return result
	}
	if info.Type != result.TaskType {
		result.State = "UNEXPECTED_TYPE"
		return result
	}
	result.State = info.State.String()
	result.Retried = info.Retried
	result.MaxRetry = info.MaxRetry
	result.Orphaned = info.IsOrphaned
	if !info.LastFailedAt.IsZero() {
		v := info.LastFailedAt.UTC()
		result.LastFailedAt = &v
	}
	if !info.CompletedAt.IsZero() {
		v := info.CompletedAt.UTC()
		result.CompletedAt = &v
	}
	return result
}
func taskType(event string) string {
	if event == "media.scan_requested" {
		return queue.TypeMediaScan
	}
	if event == "media.transcode_requested" {
		return queue.TypeMediaTranscode
	}
	return "UNKNOWN"
}
func safeRuntime(in map[string]map[string]string) map[string]map[string]string {
	allowed := map[string]bool{"APP_ENV": true, "MEDIA_SCANNER_MODE": true, "MEDIA_OPERATING_MODE": true, "REDIS_ADDR": true, "S3_ENDPOINT": true, "S3_BUCKET": true}
	out := map[string]map[string]string{}
	for _, role := range []string{"api", "worker"} {
		values := map[string]string{}
		for k, v := range in[role] {
			if allowed[k] {
				values[k] = v
			}
		}
		out[role] = values
	}
	return out
}
func apiLogs(path string) []HTTPLog {
	out := []HTTPLog{}
	eachJSON(path, func(v map[string]any) {
		route, _ := v["route_template"].(string)
		if !strings.HasPrefix(route, "/api/v1/media/") {
			return
		}
		entry := HTTPLog{Timestamp: str(v, "timestamp"), Method: str(v, "method"), Route: route, Status: num(v, "status"), DurationMS: num(v, "duration_ms")}
		out = append(out, entry)
	})
	return out
}
func workerLogs(path string) []WorkerLog {
	out := []WorkerLog{}
	eachJSON(path, func(v map[string]any) {
		msg, _ := v["msg"].(string)
		if msg != "worker_lifecycle" && msg != "worker_failed" {
			return
		}
		out = append(out, WorkerLog{Timestamp: str(v, "timestamp"), Phase: str(v, "phase"), Operation: str(v, "operation"), ErrorClass: str(v, "error_class"), JobType: str(v, "job_type"), RetryCount: num(v, "retry_count")})
	})
	return out
}
func eachJSON(path string, use func(map[string]any)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 16*1024 {
			continue
		}
		var v map[string]any
		if json.Unmarshal(scanner.Bytes(), &v) == nil {
			use(v)
		}
	}
}
func str(v map[string]any, k string) string { s, _ := v[k].(string); return s }
func num(v map[string]any, k string) int    { f, _ := v[k].(float64); return int(f) }
func Write(path string, artifact Artifact) error {
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
func ReadInput(path string) (Input, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Input{}, err
	}
	var in Input
	if err := json.Unmarshal(data, &in); err != nil {
		return Input{}, err
	}
	if strings.TrimSpace(in.RunID) == "" {
		return Input{}, fmt.Errorf("run ID is required")
	}
	return in, nil
}
