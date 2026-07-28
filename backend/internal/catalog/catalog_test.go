package catalog

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

func TestConstructorsRefuseNilDependencies(t *testing.T) {
	key := make([]byte, 32)
	writer, _ := outbox.NewWriter("key-v1", key)

	if _, err := NewRepository(nil, writer); err == nil {
		t.Error("NewRepository(nil, writer) accepted nil pool")
	}

	if _, err := NewRepository(&pgxpool.Pool{}, nil); err == nil {
		t.Error("NewRepository(pool, nil) accepted nil writer")
	}

	if _, err := NewNotificationIntentWriter(nil); err == nil {
		t.Error("NewNotificationIntentWriter(nil) accepted nil outbox writer")
	}
}

func TestMandatoryTransactionEnforcement(t *testing.T) {
	ctx := context.Background()

	var repo *Repository
	if _, err := repo.LockCourse(ctx, nil, "course-1"); err == nil {
		t.Error("LockCourse with nil tx was allowed")
	}

	event := AuditEvent{
		Action:          "COURSE_PUBLISHED",
		ActorRole:       "ADMIN",
		ActorDescriptor: "admin@example.com",
		TargetType:      "COURSE",
		TargetID:        "course-1",
		Reason:          "Approved",
	}

	if err := WriteAuditEvent(ctx, nil, event); err == nil {
		t.Error("WriteAuditEvent with nil tx was allowed")
	}

	writer := &NotificationIntentWriter{}
	if _, err := writer.WriteIntent(ctx, nil, outbox.Event{}, nil); err == nil {
		t.Error("WriteIntent with nil tx was allowed")
	}
}
