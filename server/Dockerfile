## Using Dockerfile from the following post: https://medium.com/@petomalina/using-go-mod-download-to-speed-up-golang-docker-builds-707591336888
## Modified to support Agora Golang SDK (requires CGO and native libraries)

FROM --platform=linux/amd64 golang:1.21 as build-env

# All these steps will be cached
RUN mkdir /server
WORKDIR /server

# Copy vendored Agora SDK first (needed for go.mod replace directive)
COPY vendor_sdk /server/vendor_sdk

# Copy Agora SDK headers and libraries (needed for CGO compilation)
COPY agora_sdk /server/agora_sdk

# Copy all source code
COPY . .

# Update go.mod and download dependencies
# GOPROXY=direct prevents proxy caching issues
ENV GOPROXY=direct,https://proxy.golang.org,direct
RUN go mod tidy && go mod download

# Build the binary with CGO enabled for Agora SDK
# Set CGO flags to find Agora SDK headers and libraries
ENV CGO_CFLAGS="-I/server/agora_sdk/include/c/api2 -I/server/agora_sdk/include/c/base"
ENV CGO_LDFLAGS="-L/server/agora_sdk -lagora_rtc_sdk"

# Build for x86-64 (Agora SDK is x86-64 only)
# Build main HTTP server
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -a -o /go/bin/server /server/cmd/video_conferencing

# Build bot_worker child process (runs Agora SDK in isolation)
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -a -o /go/bin/bot_worker /server/cmd/bot_worker

# Second step to build image with required libraries
FROM --platform=linux/amd64 ubuntu:22.04

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy built binaries
COPY --from=build-env /go/bin/server /go/bin/server
COPY --from=build-env /go/bin/bot_worker /go/bin/bot_worker
COPY --from=build-env /server/config.json config.json

# Create empty migrations directory (stub mode - no SQL migrations)
RUN mkdir -p migrations

# Copy Agora SDK libraries
COPY agora_sdk/*.so /usr/local/lib/

# Set library path
ENV LD_LIBRARY_PATH=/usr/local/lib:$LD_LIBRARY_PATH

ENTRYPOINT ["/go/bin/server"]
