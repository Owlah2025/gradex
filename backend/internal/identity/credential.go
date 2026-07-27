package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/Owlah2025/gradex/backend/internal/config"
)

type credentialPreparationMode uint8

const (
	credentialPrepareNew credentialPreparationMode = iota
	credentialPrepareMandatoryChange
	credentialPrepareVoluntaryChange
	credentialVerifyStored
)

var errCredentialMismatch = errors.New("credential does not match stored hash")

type credentialPreparation struct {
	next       config.Secret
	current    config.Secret
	storedHash string
	mode       credentialPreparationMode
}

// prepareCredential is the single production boundary at which password
// plaintexts are read.
//
// Link 4 added a second legitimate consumption — verifying the *current*
// password for a voluntary change — alongside validating and hashing the new
// one. Rather than let a second Expose() site appear elsewhere, both live here,
// in one function, so the reviewed surface stays one function in one file.
//
// Both plaintexts exist as locals for this function's duration only. Neither is
// returned, stored in a struct or database record, placed in an error, logged,
// written to Audit metadata, copied into an outbox event, or retained for a
// later transaction step. The only value that leaves is the encoded Argon2id
// hash of the new password, wrapped in a Secret.
//
// scripts/expose-guard.sh asserts that the marker below appears exactly once in
// production code, that it is in this file, and that every Expose() call in this
// file sits inside this function. Adding a plaintext read anywhere else fails
// CI.
//
// gradex:plaintext-boundary
func prepareCredential(
	ctx context.Context,
	preparation credentialPreparation,
	source CompromisedRangeSource,
) (config.Secret, error) {
	nextPlaintext := preparation.next.Expose()

	if preparation.mode == credentialVerifyStored {
		if preparation.storedHash == "" {
			return config.Secret{}, errors.New("stored credential hash is required for verification")
		}
		matches, err := VerifyPassword(preparation.storedHash, nextPlaintext)
		if err != nil {
			return config.Secret{}, fmt.Errorf("verifying stored credential: %w", err)
		}
		if !matches {
			return config.Secret{}, errCredentialMismatch
		}
		return config.Secret{}, nil
	}

	// The current password is verified before the new one is validated, so a
	// caller who cannot prove the existing credential learns nothing about the
	// password policy from the shape of the error.
	if preparation.mode == credentialPrepareVoluntaryChange {
		currentPlaintext := preparation.current.Expose()
		if currentPlaintext == "" {
			return config.Secret{}, ErrCurrentPasswordRequired
		}
		ok, err := VerifyPassword(preparation.storedHash, currentPlaintext)
		if err != nil {
			// A stored hash that cannot be parsed is an operational problem,
			// not a wrong password, and must not be reported as one.
			return config.Secret{}, fmt.Errorf("verifying the current password: %w", err)
		}
		if !ok {
			return config.Secret{}, ErrCurrentPasswordIncorrect
		}
		if currentPlaintext == nextPlaintext {
			return config.Secret{}, fmt.Errorf("%w: the new password matches the current one", ErrPasswordPolicy)
		}
	}

	if err := ValidatePassword(nextPlaintext); err != nil {
		return config.Secret{}, err
	}
	if err := screenCompromised(ctx, nextPlaintext, source); err != nil {
		return config.Secret{}, err
	}

	// Reusing the password already on the credential is refused even when the
	// caller did not have to supply it — otherwise the bootstrap mandatory
	// change could be "completed" by re-entering the temporary password, which
	// would satisfy the workflow while changing nothing.
	if preparation.storedHash != "" {
		same, err := VerifyPassword(preparation.storedHash, nextPlaintext)
		if err == nil && same {
			return config.Secret{}, fmt.Errorf("%w: the new password matches the current one", ErrPasswordPolicy)
		}
	}

	return HashPassword(nextPlaintext)
}

func PrepareNewCredential(ctx context.Context, newPassword config.Secret, source CompromisedRangeSource) (config.Secret, error) {
	return prepareCredential(
		ctx,
		credentialPreparation{
			next: newPassword, mode: credentialPrepareNew,
		},
		source,
	)
}

// verifyStoredCredential deliberately reuses prepareCredential so the
// supplied Secret is exposed only inside the repository's one reviewed
// password-plaintext boundary.
func verifyStoredCredential(ctx context.Context, supplied config.Secret, storedHash string) error {
	_, err := prepareCredential(
		ctx,
		credentialPreparation{
			next: supplied, storedHash: storedHash, mode: credentialVerifyStored,
		},
		nil,
	)
	return err
}
