package expr

import (
	"math"
	"testing"
)

const floatTolerance = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < floatTolerance
}

func TestEval(t *testing.T) {
	t.Parallel()

	type testCase struct {
		input   string
		want    float64
		wantErr bool
	}

	cases := []testCase{
		// Required success cases.
		{input: "1+2", want: 3},
		{input: "2*3+4", want: 10},
		{input: "2+3*4", want: 14},
		{input: "(2+3)*4", want: 20},
		{input: "-3 + 2", want: -1},
		{input: "2 * (3 + (4 - 1)) / 3", want: 4},
		{input: "1.5 + 2.5", want: 4.0},

		// Required error cases.
		{input: "", wantErr: true},
		{input: "1+", wantErr: true},
		{input: "1/0", wantErr: true},
		{input: "(1+2", wantErr: true},
		{input: "1 @ 2", wantErr: true},

		// Additional edge cases.
		{input: ".5 + .5", want: 1.0},
		{input: "2e3", want: 2000},
		{input: "1.5e2", want: 150},
		{input: "0.5e-1", want: 0.05},
		{input: "(42)", want: 42},
		{input: "10 - 3 - 2", want: 5},    // left-associativity: (10-3)-2
		{input: "8 / 4 / 2", want: 1},     // left-associativity: (8/4)/2
		{input: "-1 * -1", wantErr: true}, // second '-' is in unary position after '*'; --3 style disallowed
		{input: "1 + + 2", wantErr: true}, // unary plus not supported
		{input: "--3", wantErr: true},     // double unary minus not supported
		{input: "   ", wantErr: true},     // whitespace-only is empty
		{input: ")", wantErr: true},       // unmatched close paren
		{input: "((1+2)", wantErr: true},  // unmatched open paren
		{input: "1 2", wantErr: true},     // two numbers with no operator
		{input: "* 2", wantErr: true},     // leading binary operator
	}

	for _, tc := range cases {
		tc := tc // capture for parallel sub-tests
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := Eval(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("Eval(%q) = %v, nil; want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("Eval(%q) returned unexpected error: %v", tc.input, err)
				return
			}
			if !approxEqual(got, tc.want) {
				t.Errorf("Eval(%q) = %v; want %v (tolerance %v)", tc.input, got, tc.want, floatTolerance)
			}
		})
	}
}
