package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/db"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

const (
	applyAcknowledgement        = "REVOKE_HISTORICAL_DEVICE_SESSIONS"
	staffCleanupAcknowledgement = "REVOKE_EXPIRED_STAFF_LEGACY_SESSIONS"
)

type staffCleanupCommand struct {
	mode             string
	expectedSchema   int64
	expectedEligible int
	operator         string
	requestID        string
	confirmation     string
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "device-cutover:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	mode := flag.String("mode", "check", "check, apply, check-expired-staff-legacy, or apply-expired-staff-legacy")
	expectedSchema := flag.Int64("expected-schema", -1, "exact clean schema version for apply")
	expectedLegacy := flag.Int("expected-legacy", -1, "exact active LEGACY_UNBOUND count")
	expectedPending := flag.Int("expected-pending", -1, "exact active PENDING_DEVICE_TRUST count")
	expectedOTP := flag.Int("expected-otp", -1, "exact outstanding DEVICE_TRUST_OTP count")
	expectedExpiredStaffLegacy := flag.Int("expected-expired-staff-legacy", -1, "exact eligible expired Admin/Instructor LEGACY_UNBOUND count")
	operator := flag.String("operator", "", "release operator descriptor")
	requestID := flag.String("request-id", "", "change ticket or release request ID")
	confirm := flag.String("confirm", "", "required exact apply acknowledgement")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *mode != "check" && *mode != "apply" &&
		*mode != "check-expired-staff-legacy" && *mode != "apply-expired-staff-legacy" {
		return errors.New("mode must be check, apply, check-expired-staff-legacy, or apply-expired-staff-legacy")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer pool.Close()
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	state, err := db.ReadSchemaState(checkCtx, pool)
	if err != nil {
		return err
	}
	if state.Dirty {
		return errors.New("schema is dirty; device cutover refuses")
	}
	if state.Version < db.StudentTrustedDeviceSchemaVersion ||
		state.Version > db.AutoDeviceReplacementSchemaVersion {
		return fmt.Errorf("schema %d is outside device-policy range", state.Version)
	}
	if *mode == "check-expired-staff-legacy" || *mode == "apply-expired-staff-legacy" {
		return runExpiredStaffLegacyMode(ctx, pool, state.Version, staffCleanupCommand{
			mode: *mode, expectedSchema: *expectedSchema,
			expectedEligible: *expectedExpiredStaffLegacy,
			operator:         *operator, requestID: *requestID, confirmation: *confirm,
		})
	}
	if *mode == "check" {
		counts, err := identity.ReadDeviceCutoverCounts(checkCtx, pool)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			SchemaVersion int64                        `json:"schema_version"`
			Counts        identity.DeviceCutoverCounts `json:"counts"`
		}{state.Version, counts})
	}
	if *confirm != applyAcknowledgement {
		return errors.New("apply requires -confirm=" + applyAcknowledgement)
	}
	if *expectedSchema < db.StudentTrustedDeviceSchemaVersion ||
		*expectedSchema > db.ActiveProcessingKindSchemaVersion ||
		state.Version != *expectedSchema {
		return fmt.Errorf("apply requires the exact clean pre-43 schema; found %d", state.Version)
	}
	applyCtx, cancelApply := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelApply()
	applied, err := identity.ApplyDeviceCutover(applyCtx, pool, identity.DeviceCutoverRequest{
		Expected: identity.DeviceCutoverCounts{
			LegacySessions: *expectedLegacy, PendingSessions: *expectedPending,
			OutstandingOTP: *expectedOTP,
		},
		Operator: *operator, RequestID: *requestID,
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(applied)
}

func runExpiredStaffLegacyMode(
	ctx context.Context, pool *pgxpool.Pool, schemaVersion int64, command staffCleanupCommand,
) error {
	if schemaVersion != db.ActiveProcessingKindSchemaVersion {
		return fmt.Errorf("expired Staff cleanup requires clean schema %d; found %d", db.ActiveProcessingKindSchemaVersion, schemaVersion)
	}
	if command.mode == "check-expired-staff-legacy" {
		checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return writeExpiredStaffLegacyCheck(checkCtx, pool, schemaVersion)
	}
	return applyExpiredStaffLegacyCutover(ctx, pool, schemaVersion, command)
}

func writeExpiredStaffLegacyCheck(ctx context.Context, pool *pgxpool.Pool, schemaVersion int64) error {
	counts, err := identity.ReadExpiredStaffLegacyCleanupCounts(ctx, pool)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		SchemaVersion int64                                    `json:"schema_version"`
		Counts        identity.ExpiredStaffLegacyCleanupCounts `json:"counts"`
	}{schemaVersion, counts})
}

func applyExpiredStaffLegacyCutover(
	ctx context.Context, pool *pgxpool.Pool, schemaVersion int64, command staffCleanupCommand,
) error {
	if err := validateExpiredStaffCleanupCommand(schemaVersion, command); err != nil {
		return err
	}
	applyCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return executeExpiredStaffCleanup(applyCtx, pool, command)
}

func validateExpiredStaffCleanupCommand(schemaVersion int64, command staffCleanupCommand) error {
	if command.confirmation != staffCleanupAcknowledgement {
		return errors.New("Staff cleanup requires -confirm=" + staffCleanupAcknowledgement)
	}
	if command.expectedSchema != schemaVersion {
		return fmt.Errorf("Staff cleanup requires the exact clean schema %d; found %d", db.ActiveProcessingKindSchemaVersion, schemaVersion)
	}
	return nil
}

func executeExpiredStaffCleanup(ctx context.Context, pool *pgxpool.Pool, command staffCleanupCommand) error {
	applied, err := identity.ApplyExpiredStaffLegacyCleanup(ctx, pool, identity.ExpiredStaffLegacyCleanupRequest{
		ExpectedEligible: command.expectedEligible,
		Operator:         command.operator,
		RequestID:        command.requestID,
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(applied)
}
