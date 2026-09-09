package media

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// appendTranscodeWork is the one committed-outbox path for every approved
// scan result. Both automated scanner work and the documented LG-014 Admin
// evidence mode use it, so neither can introduce a direct queue path.
func appendTranscodeWork(ctx context.Context, tx pgx.Tx, writer *outbox.Writer, assetVersionID, correlation string) error {
	return appendTranscodeWorkAt(ctx, tx, writer, workSchedule{assetVersionID: assetVersionID, correlation: correlation})
}

type workSchedule struct {
	assetVersionID string
	kind           AssetKind
	correlation    string
	availableAt    *time.Time
}

func appendTranscodeWorkAt(ctx context.Context, tx pgx.Tx, writer *outbox.Writer, schedule workSchedule) error {
	eventID := uuid.NewString()
	_, err := writer.Append(ctx, tx, outbox.Event{
		ID: eventID, Type: "media.transcode_requested", SchemaVersion: 1,
		SourceModule: mediaSourceModule, AggregateType: "MEDIA_ASSET_VERSION",
		AggregateID: schedule.assetVersionID, AggregateRevision: 1, CorrelationID: schedule.correlation, AvailableAt: schedule.availableAt,
		SafePayload: map[string]any{"asset_version_id": schedule.assetVersionID, "operation_id": eventID},
	}, TranscodeWork{AssetVersionID: schedule.assetVersionID, OperationID: eventID})
	if err != nil {
		return fmt.Errorf("writing media transcode outbox intent: %w", err)
	}
	return nil
}

func appendScanWorkAt(ctx context.Context, tx pgx.Tx, writer *outbox.Writer, schedule workSchedule) error {
	eventID := uuid.NewString()
	_, err := writer.Append(ctx, tx, outbox.Event{
		ID: eventID, Type: "media.scan_requested", SchemaVersion: 1,
		SourceModule: mediaSourceModule, AggregateType: "MEDIA_ASSET_VERSION",
		AggregateID: schedule.assetVersionID, AggregateRevision: 1, CorrelationID: schedule.correlation, AvailableAt: schedule.availableAt,
		SafePayload: map[string]any{"asset_version_id": schedule.assetVersionID, "kind": schedule.kind, "scan_work_id": eventID},
	}, ScanWork{AssetVersionID: schedule.assetVersionID, ScanWorkID: eventID})
	if err != nil {
		return fmt.Errorf("writing media scan outbox intent: %w", err)
	}
	return nil
}
