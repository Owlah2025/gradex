package learning

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryRejectsMissingDatabaseDependency(t *testing.T) {
	if _, err := NewRepository(nil); err == nil {
		t.Fatal("repository accepted a nil database dependency")
	}
	var nilRepository *Repository
	if _, err := nilRepository.EnrollmentID(context.Background(), "student", "course"); !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("nil repository enrollment resolution = %v, want ErrEnrollmentNotFound", err)
	}
	pool, err := pgxpool.New(context.Background(), "postgres://gradex:gradex@127.0.0.1:1/gradex?sslmode=disable")
	if err != nil {
		t.Fatalf("creating lazy pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repository, err := NewRepository(pool)
	if err != nil {
		t.Fatalf("constructing repository: %v", err)
	}
	if _, err := repository.EnrollmentForLesson(context.Background(), "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"); err == nil || errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("dependency failure = %v, want a non-not-found failure", err)
	}
}
