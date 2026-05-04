package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func main() {
	qdrantHost := os.Getenv("QDRANT_HOST")
	if qdrantHost == "" {
		qdrantHost = "qdrant"
	}

	q := newQdrantClient(qdrantHost)
	q.waitUntilUp()
	log.Println("qdrant is up")
	q.waitUntilLoaded()
	log.Println("API ready")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ready", handleReady(q))
	mux.HandleFunc("POST /fraud-score", handleFraudScore(q))

	log.Fatal(http.ListenAndServe(":8080", mux))
}

func handleReady(q *QdrantClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count, err := q.pointCount()
		if err == nil && count >= totalExpected {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}
}

func handleFraudScore(q *QdrantClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req FraudRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, fraudResponse{Approved: true, FraudScore: 0})
			return
		}

		vec := vectorize(&req)
		fraudCount, err := q.search(vec)
		if err != nil {
			writeJSON(w, fraudResponse{Approved: true, FraudScore: 0})
			return
		}

		score := float32(fraudCount) / 5.0
		writeJSON(w, fraudResponse{
			Approved:   score < 0.6,
			FraudScore: score,
		})
	}
}

type fraudResponse struct {
	Approved   bool    `json:"approved"`
	FraudScore float32 `json:"fraud_score"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
