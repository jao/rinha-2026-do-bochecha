# Qdrant Image Size Reduction — Attempts Log

Target: reduce from ~1006MB uncompressed (417MB storage: 363MB segments + 55MB WAL)

---

## Attempt 1 — Delete WAL directories entirely ❌
**What:** `find /qdrant/storage -name "wal" -exec rm -rf {} +` in Dockerfile.compact  
**Result:** Qdrant panics on startup: `Can't init WAL: No such file or directory`  
**Why it failed:** Qdrant requires the WAL *directory* to exist on startup, even if empty.

---

## Attempt 2 — Delete only WAL files, keep dirs (Dockerfile.compact) ❌
**What:** `find /qdrant/storage -path "*/wal/*" -type f -delete` in Dockerfile.compact  
**Source image:** `jpcamargo/rinha-qdrant:latest` (already-pushed image)  
**Result:** Same broken image — Qdrant still panicked  
**Why it failed:** Docker BuildKit cached the RUN layer from Attempt 1 (same Dockerfile.compact, same source image digest). The fix never actually ran — the broken cached layer was reused and pushed.

---

## Attempt 3 — Re-wrap arm64 storage with amd64 binary (Dockerfile.amd64) ❌
**What:** `FROM jpcamargo/rinha-qdrant:latest AS storage` + `FROM --platform=linux/amd64 qdrant/qdrant:v1.13.4` + COPY storage  
**Result:** Broken — arm64 HNSW index is not readable by amd64 binary  
**Why it failed:** The storage was built on arm64 (local Apple Silicon). Mixing architectures corrupts index reads.

---

## Attempt 4 — Full rebuild of original Dockerfile (no WAL stripping) ✅ (working, but large)
**What:** Rebuilt the 4-stage `qdrant/Dockerfile` from scratch with `--platform linux/amd64`  
**Result:** 1006MB uncompressed, 3M vectors indexed, status green  
**WAL:** 55MB of WAL files still present

---

## What should work (not yet tried)

### Option A — WAL cleanup inside the indexer stage
Add a `RUN` after `/prebake` in the main `Dockerfile`, before the final COPY:

```dockerfile
FROM qdrant/qdrant:v1.13.4 AS indexer
...
RUN ["/prebake"]
RUN find /qdrant/storage -path "*/wal/*" -type f -delete   # <-- add this

FROM qdrant/qdrant:v1.13.4
COPY --from=indexer /qdrant/storage /qdrant/storage        # copies merged view: segments + empty WAL dir
```

Why this avoids the caching issue: the cleanup runs inside the same build, not against a pre-pushed image. No stale external cache can interfere. The final COPY sees the merged filesystem where WAL files are gone but the dir still exists.

**Expected savings:** ~55MB off the storage layer → ~950MB total
