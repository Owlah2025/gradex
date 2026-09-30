package httpapi

import "testing"

func TestSafeCSVCellNeutralizesFormulaPrefixes(t *testing.T) {
	tests := []struct {
		name string
		cell string
		want string
	}{
		{name: "equals", cell: "=SUM(A1:A2)", want: "'=SUM(A1:A2)"},
		{name: "plus", cell: "+payload", want: "'+payload"},
		{name: "minus", cell: "-payload", want: "'-payload"},
		{name: "at", cell: "@payload", want: "'@payload"},
		{name: "tab", cell: "\tpayload", want: "'\tpayload"},
		{name: "carriage return", cell: "\rpayload", want: "'\rpayload"},
		{name: "ordinary", cell: "student@example.com", want: "student@example.com"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := safeCSVCell(testCase.cell); got != testCase.want {
				t.Fatalf("safeCSVCell(%q) = %q, want %q", testCase.cell, got, testCase.want)
			}
		})
	}
}
