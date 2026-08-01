package media

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// appendTranscodeWork is the one committed-outbox path for every approved
// scan result. Both automated scanner work and the documented LG-014 Admin
// evidence mode use it, so neither can introduce a direct queue path.
func appendTranscodeWork(ctx context.Context, tx pgx.Tx, writer *outbox.Writer, assetVersionID, correlation string) error {
	eventID := uuid.NewString()
	_, err := writer.Append(ctx, tx, outbox.Event{
		ID: eventID, Type: "media.transcode_requested", SchemaVersion: 1,
		SourceModule: mediaSourceModule, AggregateType: "MEDIA_ASSET_VERSION",
		AggregateID: assetVersionID, AggregateRevision: 1, CorrelationID: correlation,
		SafePayload: map[string]any{"asset_version_id": assetVersionID, "operation_id": eventID},
	}, TranscodeWork{AssetVersionID: assetVersionID, OperationID: eventID})
	if err != nil {
		return fmt.Errorf("writing media transcode outbox intent: %w", err)
	}
	return nil
}
