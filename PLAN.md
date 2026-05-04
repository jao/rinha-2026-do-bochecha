# Rinha de Backend 2026 — Implementation Plan

## Goal

Fraud detection API using k-NN vector search over 3M labeled reference vectors.
Score is a sum of latency (p99) and detection quality. Target: 4000+ points.

---

## Architecture

```
nginx:9999  (round-robin load balancer)
├── api1:8080  (Go — vectorize + call Qdrant)
├── api2:8080  (Go — vectorize + call Qdrant)
└── qdrant:6333  (HNSW index, pre-loaded in Docker image)
```

### Resource allocation

| Service | CPU   | Memory |
|---------|-------|--------|
| nginx   | 0.05  | 20MB   |
| qdrant  | 0.50  | 200MB  |
| api1    | 0.225 | 60MB   |
| api2    | 0.225 | 60MB   |
| **Total** | **1.0** | **340MB** |

---

## API Contract

- `GET /ready` — returns 2xx when the index is loaded and ready
- `POST /fraud-score` — receives transaction payload, returns `{ "approved": bool, "fraud_score": float }`

---

## Fraud Detection Logic

### Vectorization (14 dimensions, in order)

| Index | Name                  | Formula |
|-------|-----------------------|---------|
| 0     | `amount`              | `clamp(transaction.amount / max_amount)` |
| 1     | `installments`        | `clamp(transaction.installments / max_installments)` |
| 2     | `amount_vs_avg`       | `clamp((transaction.amount / customer.avg_amount) / amount_vs_avg_ratio)` |
| 3     | `hour_of_day`         | `hour(transaction.requested_at) / 23` (UTC) |
| 4     | `day_of_week`         | `weekday(transaction.requested_at) / 6` (Mon=0, Sun=6) |
| 5     | `minutes_since_last`  | `clamp(minutes / max_minutes)` or `-1` if `last_transaction == null` |
| 6     | `km_from_last_tx`     | `clamp(last_transaction.km_from_current / max_km)` or `-1` if null |
| 7     | `km_from_home`        | `clamp(terminal.km_from_home / max_km)` |
| 8     | `tx_count_24h`        | `clamp(customer.tx_count_24h / max_tx_count_24h)` |
| 9     | `is_online`           | `1` if `terminal.is_online` else `0` |
| 10    | `card_present`        | `1` if `terminal.card_present` else `0` |
| 11    | `unknown_merchant`    | `1` if `merchant.id` NOT in `customer.known_merchants` else `0` |
| 12    | `mcc_risk`            | `mcc_risk.json[merchant.mcc]` (default `0.5`) |
| 13    | `merchant_avg_amount` | `clamp(merchant.avg_amount / max_merchant_avg_amount)` |

`clamp(x)` = min(max(x, 0.0), 1.0)

### Normalization constants (`normalization.json`)

```json
{
  "max_amount": 10000,
  "max_installments": 12,
  "amount_vs_avg_ratio": 10,
  "max_minutes": 1440,
  "max_km": 1000,
  "max_tx_count_24h": 20,
  "max_merchant_avg_amount": 10000
}
```

### Decision

1. Query Qdrant for 5 nearest neighbors (Euclidean distance)
2. `fraud_score = count_of_fraud_labels_in_5 / 5`
3. `approved = fraud_score < 0.6`

On any internal error: return `{ "approved": true, "fraud_score": 0.0 }` — avoids HTTP 500 (weight 5 penalty), takes FP hit (weight 1) instead.

---

## Qdrant Setup

### Collection config

- Distance: Euclidean (`Euclid`)
- Vector size: 14
- Quantization: Scalar (int8) — reduces memory from 168MB to ~42MB for raw vectors
- HNSW: M=8, ef_construct=100 (tune for recall vs memory)
- On-disk payload: false (keep in RAM for speed)

### Pre-loading strategy

**Option A — startup loading (simpler):**
- API startup script downloads and inserts all 3M vectors into Qdrant
- `/ready` returns 503 until loading is complete
- Downside: ~2–5 min startup time (acceptable since `/ready` gates the test)

**Option B — baked into Docker image (faster startup):**
- Multi-stage build: run Qdrant, insert 3M vectors, snapshot the storage directory
- Bake the snapshot into the final image
- Downside: complex Dockerfile, large image size (~300MB+)

**Decision: Start with Option A, move to Option B if startup time is a problem.**

---

## Implementation Steps

1. **Scaffold** — Go module, directory structure, Dockerfile stubs
2. **Vectorizer** — implement all 14 dimension formulas + load `mcc_risk.json` / `normalization.json`
3. **Qdrant client** — insert vectors at startup, query for KNN
4. **HTTP handlers** — `/ready` + `/fraud-score` with minimal allocations
5. **Docker** — multi-stage Go build, final image with startup script
6. **docker-compose** — nginx + qdrant + 2 API instances, resource limits
7. **Correctness test** — validate against the example payloads from the repo
8. **Tune** — Qdrant HNSW `ef` parameter, nprobe, check recall vs latency tradeoff

---

## Scoring Targets

| Metric | Target | Score impact |
|--------|--------|-------------|
| p99 latency | ≤ 10ms | +2000 latency points |
| Failure rate (FP+FN+Err) | < 5% | +2000+ detection points |
| HTTP errors | 0 | Critical (weight 5 each) |

Realistic goal: **~4000 total score**.

---

## Reference Files (from the challenge repo)

- `resources/references.json.gz` — 3M labeled vectors
- `resources/mcc_risk.json` — MCC risk scores
- `resources/normalization.json` — normalization constants
- `resources/example-payloads.json` — sample requests for testing

---

## Submission Requirements

- `main` branch: source code
- `submission` branch: only files needed to run (`docker-compose.yml` at root)
- All Docker images must be public and `linux/amd64` compatible
- Open an issue with `rinha/test` in the body to trigger the official test
