package main

import (
	"compress/gzip"
	"encoding/json"
	"log"
	"os"
	"sync"
	"sync/atomic"
)

const (
	totalExpected = 3_000_000
	batchSize     = 5_000
	numWorkers    = 4
)

type refRecord struct {
	Vector [14]float32 `json:"vector"`
	Label  string      `json:"label"`
}

func loadReferences(q *QdrantClient, path string) error {
	count, err := q.pointCount()
	if err == nil && count >= totalExpected {
		log.Printf("already have %d vectors, skipping load", count)
		return nil
	}

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
	if _, err := dec.Token(); err != nil { // consume '['
		return err
	}

	type work struct{ points []QdrantPoint }
	workCh := make(chan work, numWorkers)

	var (
		wg       sync.WaitGroup
		firstErr atomic.Value
	)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range workCh {
				if err := q.upsert(w.points); err != nil {
					firstErr.CompareAndSwap(nil, err)
					log.Printf("upsert error: %v", err)
				}
			}
		}()
	}

	batch := make([]QdrantPoint, 0, batchSize)
	var id uint64

	for dec.More() {
		var rec refRecord
		if err := dec.Decode(&rec); err != nil {
			close(workCh)
			return err
		}

		vec := make([]float32, 14)
		copy(vec, rec.Vector[:])

		batch = append(batch, QdrantPoint{
			ID:      id,
			Vector:  vec,
			Payload: map[string]any{"is_fraud": rec.Label == "fraud"},
		})
		id++

		if len(batch) == batchSize {
			workCh <- work{batch}
			batch = make([]QdrantPoint, 0, batchSize)
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

	if e := firstErr.Load(); e != nil {
		return e.(error)
	}

	log.Printf("loaded %d vectors into Qdrant", id)
	return nil
}
