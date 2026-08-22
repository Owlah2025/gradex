package main

import "testing"

func TestBooleanEnvironmentUsesConfiguredValue(t *testing.T) {
	testCases := []struct {
		name     string
		value    string
		fallback bool
		want     bool
	}{
		{name: "path style", value: "true", fallback: false, want: true},
		{name: "provider style", value: "false", fallback: true, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("S3_USE_PATH_STYLE", testCase.value)

			got, err := booleanEnvironment("S3_USE_PATH_STYLE", testCase.fallback)
			if err != nil {
				t.Fatalf("booleanEnvironment: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("S3_USE_PATH_STYLE=%s resolved %t", testCase.value, got)
			}
		})
	}
}

func TestBooleanEnvironmentUsesFallbackWhenUnset(t *testing.T) {
	t.Setenv("UNRELATED_FIXTURE_BOOLEAN", "unused")

	got, err := booleanEnvironment("MISSING_FIXTURE_BOOLEAN", false)
	if err != nil {
		t.Fatalf("booleanEnvironment: %v", err)
	}
	if got {
		t.Fatal("unset setting did not preserve false fallback")
	}
}

func TestBooleanEnvironmentRejectsInvalidValue(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "malformed", value: "definitely-not-a-bool"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("S3_USE_PATH_STYLE", testCase.value)

			if _, err := booleanEnvironment("S3_USE_PATH_STYLE", false); err == nil {
				t.Fatalf("invalid boolean value %q was accepted", testCase.value)
			}
		})
	}
}
