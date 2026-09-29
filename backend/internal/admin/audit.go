package admin

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/catalog"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

type PrivilegedReadAudit struct {
	Principal     identity.Principal
	CorrelationID string
	Action        string
	Module        string
	TargetType    string
	TargetID      string
	Reason        string
	Metadata      map[string]any
}

func WritePrivilegedReadAudit(ctx context.Context, tx pgx.Tx, event PrivilegedReadAudit) error {
	if err := event.Principal.Valid(); err != nil {
		return errors.New("privileged-read actor is invalid")
	}
	if strings.TrimSpace(event.Action) == "" || strings.TrimSpace(event.Module) == "" ||
		strings.TrimSpace(event.TargetType) == "" || strings.TrimSpace(event.TargetID) == "" ||
		strings.TrimSpace(event.Reason) == "" {
		return errors.New("privileged-read audit fields are required")
	}

	actorID := event.Principal.AccountID
	var correlation *string
	if strings.TrimSpace(event.CorrelationID) != "" {
		value := strings.TrimSpace(event.CorrelationID)
		correlation = &value
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	return catalog.WriteAuditEvent(ctx, tx, catalog.AuditEvent{
		ActorAccountID:  &actorID,
		ActorRole:       string(event.Principal.Role),
		ActorDescriptor: actorID,
		Action:          event.Action,
		Module:          event.Module,
		TargetType:      event.TargetType,
		TargetID:        event.TargetID,
		Reason:          event.Reason,
		Metadata:        event.Metadata,
		CorrelationID:   correlation,
	})
}
