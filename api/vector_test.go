package main

import (
	"math"
	"testing"
)

func approx(a, b float32) bool {
	return math.Abs(float64(a-b)) < 0.001
}

func TestVectorizeExamples(t *testing.T) {
	tests := []struct {
		name     string
		req      FraudRequest
		expected [14]float32
	}{
		{
			name: "legit example from spec",
			req: FraudRequest{
				Transaction: Transaction{Amount: 41.12, Installments: 2, RequestedAt: "2026-03-11T18:45:53Z"},
				Customer:    Customer{AvgAmount: 82.24, TxCount24h: 3, KnownMerchants: []string{"MERC-003", "MERC-016"}},
				Merchant:    Merchant{ID: "MERC-016", MCC: "5411", AvgAmount: 60.25},
				Terminal:    Terminal{IsOnline: false, CardPresent: true, KmFromHome: 29.23},
				LastTx:      nil,
			},
			expected: [14]float32{0.0041, 0.1667, 0.05, 0.7826, 0.3333, -1, -1, 0.0292, 0.15, 0, 1, 0, 0.15, 0.006},
		},
		{
			name: "fraud example from spec",
			req: FraudRequest{
				Transaction: Transaction{Amount: 9505.97, Installments: 10, RequestedAt: "2026-03-14T05:15:12Z"},
				Customer:    Customer{AvgAmount: 81.28, TxCount24h: 20, KnownMerchants: []string{"MERC-008", "MERC-007", "MERC-005"}},
				Merchant:    Merchant{ID: "MERC-068", MCC: "7802", AvgAmount: 54.86},
				Terminal:    Terminal{IsOnline: false, CardPresent: true, KmFromHome: 952.27},
				LastTx:      nil,
			},
			expected: [14]float32{0.9506, 0.8333, 1.0, 0.2174, 0.8333, -1, -1, 0.9523, 1.0, 0, 1, 1, 0.75, 0.0055},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vectorize(&tt.req)
			for i, want := range tt.expected {
				if !approx(got[i], want) {
					t.Errorf("dim[%d]: got %f, want %f", i, got[i], want)
				}
			}
		})
	}
}
