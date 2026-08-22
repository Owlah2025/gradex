package main

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/identity"
)

// Bootstrap close condition 2, argv half: "the plaintext bootstrap password is
// never [...] placed in process arguments".
//
// Process arguments are visible to every other process on the host, land in
// shell history, and are captured by process supervisors. The defense is that
// no flag accepts a password — so this test asserts against the actual
// registered flag set rather than against a comment saying so, and fails if
// someone later adds one for convenience.
func TestNoFlagAcceptsAPassword(t *testing.T) {
	fs := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	fs.SetOutput(nil)
	registerFlags(fs)

	var offenders []string
	fs.VisitAll(func(f *flag.Flag) {
		name := strings.ToLower(f.Name)
		usage := strings.ToLower(f.Usage)
		for _, banned := range []string{"password", "passwd", "secret", "credential", "token"} {
			if strings.Contains(name, banned) || strings.Contains(usage, banned) {
				offenders = append(offenders, f.Name)
				return
			}
		}
	})

	if len(offenders) > 0 {
		t.Fatalf("flags accept credential material, which would place it in process arguments: %v", offenders)
	}
}

// The same guarantee, checked against the source rather than the flag set, so
// that a password read from os.Args by some other route is also caught.
func TestSourceNeverReadsAPasswordFromArgs(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing main.go: %v", err)
	}

	var problems []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != "flag" {
			return true
		}
		// Any flag.String/StringVar whose name argument mentions a credential.
		for _, arg := range call.Args {
			lit, ok := arg.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			v := strings.ToLower(lit.Value)
			if strings.Contains(v, "password") || strings.Contains(v, "secret") {
				problems = append(problems, sel.Sel.Name+"("+lit.Value+")")
			}
		}
		return true
	})

	if len(problems) > 0 {
		t.Fatalf("main.go registers credential-bearing flags: %v", problems)
	}
}

func TestParseFlagsRejectsPositionalArguments(t *testing.T) {
	if _, err := parseFlags([]string{"unexpected"}, nil); err == nil {
		t.Fatal("expected a positional argument to be rejected")
	}
}

func TestProductionBootstrapCompositionSelectsHIBP(t *testing.T) {
	settings := map[string]string{
		"APP_ENV":                               "production",
		"PUBLIC_ORIGIN":                         "https://gradex.example",
		"CORS_ALLOWED_ORIGINS":                  "https://gradex.example",
		"CORS_ALLOW_CREDENTIALS":                "true",
		"SALES_WHATSAPP_NUMBER":                 "15550000000",
		"REDIS_ADDR":                            "redis:6379",
		"REDIS_TLS_ENABLED":                     "true",
		"S3_ENDPOINT":                           "https://storage.example",
		"S3_BUCKET":                             "gradex-media",
		"AUTH_FAKE_MODE":                        "false",
		"PASSWORD_SCREEN_MODE":                  "adapter",
		"COMPROMISED_PASSWORD_ADAPTER_APPROVED": "true",
		"LEGAL_IDENTITY_MODE":                   "public",
		"LEGAL_OPERATOR_NAME":                   "Gradex Courses",
		"LEGAL_REGISTRATION_NUMBER":             "KWT-REAL-123",
		"LEGAL_REGISTERED_ADDRESS":              "Kuwait City, Kuwait",
		"PRIVACY_EMAIL":                         "privacy@gradex.example",
		"SUPPORT_EMAIL":                         "support@gradex.example",
		"SECURITY_EMAIL":                        "security@gradex.example",
	}
	secrets := config.MapSecretResolver{
		"DATABASE_URL":                 "postgres://gradex:pw@db:5432/gradex",
		"REDIS_PASSWORD":               "redis-password",
		"S3_ACCESS_KEY":                "access",
		"S3_SECRET_KEY":                "secret",
		"PLAYBACK_TOKEN_SECRET":        strings.Repeat("p", 32),
		"SESSION_CSRF_KEY":             strings.Repeat("s", 32),
		"ANONYMOUS_COOKIE_SIGNING_KEY": strings.Repeat("a", 32),
		"ANONYMOUS_CSRF_KEY":           strings.Repeat("b", 32),
		"ADMISSION_LIMITER_HMAC_KEY":   strings.Repeat("c", 32),
	}
	cfg, err := config.LoadFrom(config.MapLookup(settings), secrets)
	if err != nil {
		t.Fatalf("loading production bootstrap config: %v", err)
	}

	source, err := buildBootstrapCompromisedSource(cfg)
	if err != nil {
		t.Fatalf("building production bootstrap source: %v", err)
	}
	if source.Scheme() != identity.CompromisedSHA1V1 || source.PrefixLength() != 5 {
		t.Fatalf("bootstrap source = %s/%d, want HIBP SHA-1/5", source.Scheme(), source.PrefixLength())
	}
}
