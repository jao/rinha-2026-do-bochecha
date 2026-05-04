package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	collectionName = "tx"
	totalExpected  = 3_000_000
)

type QdrantClient struct {
	baseURL string
	client  *http.Client // short timeout — for queries and health checks
	upload  *http.Client // no timeout — for bulk upsert during loading
}

func newQdrantClient(host string) *QdrantClient {
	return &QdrantClient{
		baseURL: fmt.Sprintf("http://%s:6333", host),
		client:  &http.Client{Timeout: 10 * time.Second},
		upload:  &http.Client{},
	}
}

func (q *QdrantClient) waitUntilUp() {
	for {
		resp, err := q.client.Get(q.baseURL + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (q *QdrantClient) waitUntilLoaded() {
	for {
		count, err := q.pointCount()
		if err == nil && count >= totalExpected {
			return
		}
		time.Sleep(5 * time.Second)
	}
}

func (q *QdrantClient) createCollection() error {
	body := map[string]any{
		"vectors": map[string]any{
			"size":     14,
			"distance": "Euclid",
			"on_disk": true,
		},
		"quantization_config": map[string]any{
			"scalar": map[string]any{
				"type":       "int8",
				"quantile":   0.99,
				"always_ram": true,
			},
		},
		// HNSW graph (~74 MB for M=4, 3M vectors) is built in RAM using
		// int8 distances. Combined with 42 MB int8 + 25 MB overhead = ~141 MB.
		"hnsw_config": map[string]any{
			"m":            4,
			"ef_construct": 100,
			"on_disk":      false,
		},
	}
	return q.doPut("/collections/"+collectionName, body)
}

type QdrantPoint struct {
	ID     uint64    `json:"id"`
	Vector []float32 `json:"vector"`
	// No payload — fraud label is encoded in bit 0 of the ID.
}

func (q *QdrantClient) upsert(points []QdrantPoint) error {
	body := map[string]any{"points": points}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut,
		q.baseURL+"/collections/"+collectionName+"/points",
		bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := q.upload.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upsert status %d: %s", resp.StatusCode, b)
	}
	return nil
}

func (q *QdrantClient) search(vector [14]float32) (fraudCount int, err error) {
	body := map[string]any{
		"vector":       vector[:],
		"limit":        5,
		"with_payload": false, // fraud label is in bit 0 of the ID, no payload needed
		"params": map[string]any{
			"hnsw_ef": 64,
			"exact":   false,
			"quantization": map[string]any{
				"rescore": true,
			},
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}

	resp, err := q.client.Post(
		q.baseURL+"/collections/"+collectionName+"/points/search",
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("search %d: %s", resp.StatusCode, b)
	}

	var result struct {
		Result []struct {
			ID uint64 `json:"id"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	for _, hit := range result.Result {
		if hit.ID&1 == 1 { // bit 0 set = fraud
			fraudCount++
		}
	}
	return fraudCount, nil
}

func (q *QdrantClient) pointCount() (int64, error) {
	resp, err := q.client.Get(q.baseURL + "/collections/" + collectionName)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Result struct {
			PointsCount int64 `json:"points_count"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.Result.PointsCount, nil
}

func (q *QdrantClient) doPut(path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, q.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT %s status %d: %s", path, resp.StatusCode, b)
	}
	return nil
}
