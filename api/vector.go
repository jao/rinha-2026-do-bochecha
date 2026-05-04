package main

import "time"

const (
	maxAmount            = 10000.0
	maxInstallments      = 12.0
	amountVsAvgRatio     = 10.0
	maxMinutes           = 1440.0
	maxKm                = 1000.0
	maxTxCount24h        = 20.0
	maxMerchantAvgAmount = 10000.0
)

var mccRisk = map[string]float32{
	"5411": 0.15,
	"5812": 0.30,
	"5912": 0.20,
	"5944": 0.45,
	"7801": 0.80,
	"7802": 0.75,
	"7995": 0.85,
	"4511": 0.35,
	"5311": 0.25,
	"5999": 0.50,
}

type FraudRequest struct {
	ID          string      `json:"id"`
	Transaction Transaction `json:"transaction"`
	Customer    Customer    `json:"customer"`
	Merchant    Merchant    `json:"merchant"`
	Terminal    Terminal    `json:"terminal"`
	LastTx      *LastTx     `json:"last_transaction"`
}

type Transaction struct {
	Amount       float32 `json:"amount"`
	Installments int     `json:"installments"`
	RequestedAt  string  `json:"requested_at"`
}

type Customer struct {
	AvgAmount      float32  `json:"avg_amount"`
	TxCount24h     int      `json:"tx_count_24h"`
	KnownMerchants []string `json:"known_merchants"`
}

type Merchant struct {
	ID        string  `json:"id"`
	MCC       string  `json:"mcc"`
	AvgAmount float32 `json:"avg_amount"`
}

type Terminal struct {
	IsOnline    bool    `json:"is_online"`
	CardPresent bool    `json:"card_present"`
	KmFromHome  float32 `json:"km_from_home"`
}

type LastTx struct {
	Timestamp     string  `json:"timestamp"`
	KmFromCurrent float32 `json:"km_from_current"`
}

func clamp(x float32) float32 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func vectorize(req *FraudRequest) [14]float32 {
	var v [14]float32

	v[0] = clamp(req.Transaction.Amount / maxAmount)
	v[1] = clamp(float32(req.Transaction.Installments) / maxInstallments)

	if req.Customer.AvgAmount > 0 {
		v[2] = clamp((req.Transaction.Amount / req.Customer.AvgAmount) / amountVsAvgRatio)
	}

	t, err := time.Parse(time.RFC3339, req.Transaction.RequestedAt)
	if err == nil {
		t = t.UTC()
		v[3] = float32(t.Hour()) / 23.0
		// Go Weekday: Sun=0..Sat=6; spec wants Mon=0..Sun=6
		v[4] = float32((int(t.Weekday())+6)%7) / 6.0
	}

	if req.LastTx == nil {
		v[5] = -1
		v[6] = -1
	} else {
		v[6] = clamp(req.LastTx.KmFromCurrent / maxKm)
		lastT, err := time.Parse(time.RFC3339, req.LastTx.Timestamp)
		if err == nil {
			minutes := t.Sub(lastT).Minutes()
			if minutes < 0 {
				minutes = -minutes
			}
			v[5] = clamp(float32(minutes) / maxMinutes)
		} else {
			v[5] = -1
		}
	}

	v[7] = clamp(req.Terminal.KmFromHome / maxKm)
	v[8] = clamp(float32(req.Customer.TxCount24h) / maxTxCount24h)

	if req.Terminal.IsOnline {
		v[9] = 1
	}
	if req.Terminal.CardPresent {
		v[10] = 1
	}

	known := false
	for _, m := range req.Customer.KnownMerchants {
		if m == req.Merchant.ID {
			known = true
			break
		}
	}
	if !known {
		v[11] = 1
	}

	if risk, ok := mccRisk[req.Merchant.MCC]; ok {
		v[12] = risk
	} else {
		v[12] = 0.5
	}

	v[13] = clamp(req.Merchant.AvgAmount / maxMerchantAvgAmount)

	return v
}
