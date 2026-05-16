# Rinha de Backend 2026 — submission

Source code lives on the **main** branch:
[`https://github.com/vinnedev/rinha-2026`](https://github.com/vinnedev/rinha-2026).

This branch contains only the runtime artifacts the rinha engine needs:

- `docker-compose.yml` — pulls two prebuilt images from GHCR:
  - `ghcr.io/vinnedev/rinha-2026-api` — Go API + mmap'd VP-Tree + RandomForest
  - `ghcr.io/vinnedev/rinha-2026-lb` — Go TCP-splice LB (drop-in nginx replacement)
- `info.json` — participant info
- `LICENSE` — MIT

Stack: Go 1.26, sklearn-trained RandomForest distilled from k-NN labels, exact
VP-Tree with AVX2 distance kernel, fastjson schema parser, mmap'd indexes.
Total resources: 1.0 CPU / 350 MB (LB 0.16 / 30 MB, two APIs at 0.42 / 160 MB
each).
