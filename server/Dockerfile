## Optimized Dockerfile for Go with CGO (Agora SDK)
## Dependencies cached - only re-downloaded when go.mod/go.sum change

FROM --platform=linux/amd64 golang:1.21 as build-env

WORKDIR /server

# 1. Copy vendored SDKs (needed for replace directive and CGO)
COPY vendor_sdk /server/vendor_sdk
COPY agora_sdk /server/agora_sdk

# 2. Copy go.mod/go.sum and download dependencies (CACHED unless deps change)
COPY go.mod go.sum ./
RUN go mod download

# 3. Copy source code (changes frequently, but deps already cached above)
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
COPY pkg/ ./pkg/
COPY services/ ./services/
COPY utils/ ./utils/

# 4. Build binaries
ENV CGO_CFLAGS="-I/server/agora_sdk/include/c/api2 -I/server/agora_sdk/include/c/base"
ENV CGO_LDFLAGS="-L/server/agora_sdk -lagora_rtc_sdk"

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /go/bin/server ./cmd/video_conferencing
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o /go/bin/bot_worker ./cmd/bot_worker

# --- Runtime image ---
FROM --platform=linux/amd64 ubuntu:22.04

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=build-env /go/bin/server /go/bin/server
COPY --from=build-env /go/bin/bot_worker /go/bin/bot_worker

COPY agora_sdk/*.so /usr/local/lib/
ENV LD_LIBRARY_PATH=/usr/local/lib:$LD_LIBRARY_PATH

RUN mkdir -p migrations

ENTRYPOINT ["/go/bin/server"]
