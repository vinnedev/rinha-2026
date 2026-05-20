FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v3
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -pgo=cmd/api/default.pgo -ldflags="-s -w" -o /out/api ./cmd/api && \
    go build -trimpath -ldflags="-s -w" -o /out/lb ./cmd/lb && \
    go build -trimpath -ldflags="-s -w" -o /out/preprocess ./cmd/preprocess

FROM build AS prep
ARG REFS_URL=https://github.com/zanfranceschi/rinha-de-backend-2026/raw/main/resources/references.json.gz
RUN mkdir -p /work && \
    if [ -f /src/resources/references.json.gz ]; then \
      cp /src/resources/references.json.gz /work/refs.json.gz; \
    else \
      wget -q -O /work/refs.json.gz "$REFS_URL"; \
    fi && \
    /out/preprocess /work/refs.json.gz /work/vectors.bin && \
    rm -f /work/refs.json.gz && \
    if [ -f /src/resources/fraud_dt.bin ]; then \
      cp /src/resources/fraud_dt.bin /work/fraud_dt.bin; \
    else \
      echo "WARN: resources/fraud_dt.bin not found in build context (run model/train.py + export_to_go.py first)"; \
      exit 1; \
    fi

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
COPY --from=build /out/lb /lb
COPY --from=prep --chown=nonroot:nonroot /work/vectors.bin /data/vectors.bin
COPY --from=prep --chown=nonroot:nonroot /work/fraud_dt.bin /data/fraud_dt.bin
USER nonroot:nonroot
EXPOSE 8080
ENV DATASET_PATH=/data/vectors.bin
ENV TREE_PATH=/data/fraud_dt.bin
ENTRYPOINT ["/api"]
