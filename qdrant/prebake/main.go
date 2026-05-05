package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	qdrantURL      = "http://localhost:6333"
	collectionName = "tx"
	totalExpected  = 3_000_000
	batchSize      = 10_000
	numWorkers     = 8
	referencesPath = "/data/references.json.gz"
	qdrantBin      = "/qdrant/qdrant"
	qdrantDir      = "/qdrant"
	storagePath    = "/qdrant/storage"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}
var uploadClient = &http.Client{}

func main() {
	cmd := exec.Command(qdrantBin)
	cmd.Dir = qdrantDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"QDRANT__STORAGE__STORAGE_PATH="+storagePath,
		"QDRANT__TELEMETRY_DISABLED=true",
	)
	if err := cmd.Start(); err != nil {
		log.Fatalf("start qdrant: %v", err)
	}

	waitForQdrant()

	if err := createCollection(); err != nil {
		log.Printf("createCollection (may already exist): %v", err)
	}

	if err := loadReferences(referencesPath); err != nil {
		log.Fatalf("loadReferences: %v", err)
	}

	waitForIndexing()

	log.Println("pre-bake complete — stopping qdrant")
	cmd.Process.Signal(os.Interrupt)
	cmd.Wait()
}

func waitForQdrant() {
	log.Println("waiting for qdrant...")
	for {
		resp, err := httpClient.Get(qdrantURL + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				log.Println("qdrant is up")
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func createCollection() error {
	body := map[string]any{
		"vectors": map[string]any{
			"size":     14,
			"distance": "Euclid",
			"on_disk":  true,
		},
		"quantization_config": map[string]any{
			"scalar": map[string]any{
				"type":       "int8",
				"quantile":   0.99,
				"always_ram": true,
			},
		},
		"hnsw_config": map[string]any{
			"m":            4,
			"ef_construct": 100,
			"on_disk":      false,
		},
	}
	return doPut("/collections/"+collectionName, body)
}

type point struct {
	ID     uint64    `json:"id"`
	Vector []float32 `json:"vector"`
}

func upsert(points []point) error {
	body := map[string]any{"points": points}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut,
		qdrantURL+"/collections/"+collectionName+"/points",
		bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := uploadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upsert %d: %s", resp.StatusCode, b)
	}
	return nil
}

func upsertWithRetry(pts []point) {
	for attempt := 0; ; attempt++ {
		if err := upsert(pts); err == nil {
			return
		} else {
			wait := time.Duration(1<<uint(attempt)) * time.Second
			if wait > 30*time.Second {
				wait = 30 * time.Second
			}
			log.Printf("upsert error (retry in %v): %v", wait, err)
			time.Sleep(wait)
		}
	}
}

type refRecord struct {
	Vector [14]float32 `json:"vector"`
	Label  string      `json:"label"`
}

func loadReferences(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	dec := json.NewDecoder(gz)
	if _, err := dec.Token(); err != nil {
		return err
	}

	type work struct{ points []point }
	workCh := make(chan work, numWorkers)

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range workCh {
				upsertWithRetry(w.points)
			}
		}()
	}

	batch := make([]point, 0, batchSize)
	var id uint64

	for dec.More() {
		var rec refRecord
		if err := dec.Decode(&rec); err != nil {
			close(workCh)
			return err
		}

		vec := make([]float32, 14)
		copy(vec, rec.Vector[:])

		pointID := id<<1 | boolToUint64(rec.Label == "fraud")
		batch = append(batch, point{ID: pointID, Vector: vec})
		id++

		if len(batch) == batchSize {
			workCh <- work{batch}
			batch = make([]point, 0, batchSize)
		}

		if id%500_000 == 0 {
			log.Printf("queued %d / %d vectors", id, totalExpected)
		}
	}

	if len(batch) > 0 {
		workCh <- work{batch}
	}
	close(workCh)
	wg.Wait()

	log.Printf("uploaded %d vectors", id)
	return nil
}

func waitForIndexing() {
	log.Println("waiting for HNSW indexing to complete...")
	var lastCount int64
	stableRounds := 0
	for {
		time.Sleep(15 * time.Second)
		indexed, _, err := collectionStats()
		if err != nil {
			log.Printf("stats error: %v", err)
			continue
		}
		log.Printf("indexed %d vectors", indexed)
		if indexed >= totalExpected {
			log.Println("HNSW indexing complete")
			return
		}
		// Qdrant won't index segments below indexing_threshold (default 20K).
		// If the count has been stable for 4 polls (~60s), the optimizer is done.
		if indexed > 0 && indexed == lastCount {
			stableRounds++
			if stableRounds >= 4 {
				log.Printf("optimizer plateaued at %d — proceeding", indexed)
				return
			}
		} else {
			stableRounds = 0
		}
		lastCount = indexed
	}
}

func collectionStats() (indexed, total int64, err error) {
	resp, err := httpClient.Get(qdrantURL + "/collections/" + collectionName)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Result struct {
			IndexedVectorsCount int64 `json:"indexed_vectors_count"`
			VectorsCount        int64 `json:"vectors_count"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, 0, err
	}
	return result.Result.IndexedVectorsCount, result.Result.VectorsCount, nil
}

func doPut(path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, qdrantURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT %s %d: %s", path, resp.StatusCode, b)
	}
	return nil
}

func boolToUint64(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}
