package identity

import (
	"errors"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

var (
	ErrAdmissionUnavailable = errors.New("Student admission is unavailable")
	ErrDeliveryUnavailable  = errors.New("transactional delivery is unavailable")
	ErrInvalidLocale        = errors.New("locale is invalid")
)

type StudentRegistration struct {
	DisplayName string
	Email       string
	Password    config.Secret
	Locale      Locale
	PolicySetID string
	RequestID   string
}

type VerificationRequest struct {
	Email     string
	RequestID string
}

type AdmissionServiceOptions struct {
	Pool            *pgxpool.Pool
	Policies        PolicySetResolver
	Compromised     CompromisedRangeSource
	Outbox          *outbox.Writer
	VerificationTTL time.Duration
	Now             func() time.Time
	Random          io.Reader
}

func validateRequestID(raw string) (string, error) {
	requestID := strings.TrimSpace(raw)
	if requestID == "" || len(requestID) > 200 {
		return "", errors.New("trusted request ID is invalid")
	}
	return requestID, nil
}
