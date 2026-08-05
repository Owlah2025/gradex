package learning

import (
	"reflect"
	"testing"
)

func TestBoundPositionClampsUntrustedInput(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		position float64
		duration float64
		want     float64
	}{
		{name: "negative", position: -1, duration: 100, want: 0},
		{name: "within duration", position: 25, duration: 100, want: 25},
		{name: "beyond duration", position: 101, duration: 100, want: 100},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := BoundPosition(testCase.position, testCase.duration); got != testCase.want {
				t.Fatalf("BoundPosition(%v, %v) = %v, want %v", testCase.position, testCase.duration, got, testCase.want)
			}
		})
	}
}

func TestCompletesUsesTrustedDurationThreshold(t *testing.T) {
	if Completes(89.999, 100) {
		t.Fatal("position below ninety percent completed a lesson")
	}
	if !Completes(90, 100) {
		t.Fatal("position at ninety percent did not complete a lesson")
	}
	if !Completes(100, 100) {
		t.Fatal("position at trusted duration did not complete a lesson")
	}
	if Completes(1, 0) {
		t.Fatal("zero trusted duration completed a lesson")
	}
	if Completes(90, -100) {
		t.Fatal("invalid trusted duration completed a lesson")
	}
}

func TestProgressWriteCannotCarryClientCompletionClaims(t *testing.T) {
	writeType := reflect.TypeOf(ProgressWrite{})
	for _, forbidden := range []string{"Duration", "DurationSeconds", "Completion", "CompletionPercent", "Percentage"} {
		if _, present := writeType.FieldByName(forbidden); present {
			t.Fatalf("ProgressWrite accepts forbidden client completion claim %q", forbidden)
		}
	}
	if !Completes(BoundPosition(5_000, 100), 100) {
		t.Fatal("trusted position and duration did not determine completion")
	}
}
