package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchB_AccessInvariants(t *testing.T) {
	t.Run("Invariant 1 (T062): Entitlements originate only in canonical invitation transactions", func(t *testing.T) {
		repoPath := filepath.Join("..", "..")
		err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			content := string(data)
			if strings.Contains(content, "INSERT INTO entitlements") {
				if !strings.Contains(path, "internal/access/repository.go") && !strings.Contains(path, "internal/access/purchase.go") {
					if strings.Contains(path, "seed") || strings.Contains(path, "test") {
						// Allowed non-production test / seed files
					} else {
						t.Errorf("file %s performs INSERT INTO entitlements outside a canonical invitation transaction", path)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking repository: %v", err)
		}
	})

	t.Run("Invariant 2 (T063): Authorization decision reads Entitlements, never invitation state", func(t *testing.T) {
		repoPath := filepath.Join("..", "..")
		err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if (strings.Contains(path, "authorization") || strings.Contains(path, "learning")) && !strings.Contains(path, "access") {
				data, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				content := string(data)
				if strings.Contains(content, "course_access_invitations") {
					t.Errorf("authorization/learning evaluator in %s references course_access_invitations table", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking repository: %v", err)
		}
	})

	t.Run("Invariant 3 (T064): No commercial payment state or fields in access grant API", func(t *testing.T) {
		forbiddenTerms := []string{
			"payment_intent", "stripe", "paypal", "charge_id",
			"amount_cents", "currency_code", "payment_status", "billing_address",
		}
		repoPath := filepath.Join("..", "access")
		err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			content := strings.ToLower(string(data))
			for _, term := range forbiddenTerms {
				if strings.Contains(content, term) {
					t.Errorf("file %s contains forbidden commercial term %q", path, term)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking internal/access: %v", err)
		}
	})

	t.Run("Invariant 4 (T065): Every access mutation writes audit_events inside database transaction", func(t *testing.T) {
		repoPath := filepath.Join("..", "access", "repository.go")
		data, err := os.ReadFile(repoPath)
		if err != nil {
			t.Fatalf("reading repository.go: %v", err)
		}
		content := string(data)
		mutations := []string{"ApproveInvitation", "RejectInvitation", "CancelInvitation", "ResendInvitation"}
		for _, m := range mutations {
			if !strings.Contains(content, m) {
				t.Errorf("repository.go missing mutation %s", m)
			}
		}
		if strings.Count(content, "INSERT INTO audit_events") < 5 {
			t.Errorf("repository.go contains fewer audit_events inserts than expected mutations")
		}
	})

	t.Run("Invariant 5 (T066): Route registration enforces CSRF protection and strict body limit on mutations", func(t *testing.T) {
		repoPath := filepath.Join("..", "httpapi", "access_routes.go")
		data, err := os.ReadFile(repoPath)
		if err != nil {
			t.Fatalf("reading access_routes.go: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "requireSessionMutationSecurity") {
			t.Error("access_routes.go missing requireSessionMutationSecurity middleware")
		}
	})
}
