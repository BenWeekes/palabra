## Using Dockerfile from the following post: https://medium.com/@petomalina/using-go-mod-download-to-speed-up-golang-docker-builds-707591336888
## Modified to support Agora Golang SDK (requires CGO and native libraries)

FROM --platform=linux/amd64 golang:1.21 as build-env

# All these steps will be cached
RUN mkdir /server
WORKDIR /server

# Copy vendored Agora SDK first (needed for go.mod replace directive)
COPY vendor_sdk /server/vendor_sdk

# Copy go.mod files
COPY go.mod .
COPY go.sum .

# Get dependancies - will also be cached if we won't change mod/sum
RUN go mod download

# Copy Agora SDK headers and libraries (needed for CGO compilation)
COPY agora_sdk /server/agora_sdk

# COPY the source code as the last step
COPY . .

# Build the binary with CGO enabled for Agora SDK
# Set CGO flags to find Agora SDK headers and libraries
ENV CGO_CFLAGS="-I/server/agora_sdk/include/c/api2 -I/server/agora_sdk/include/c/base"
ENV CGO_LDFLAGS="-L/server/agora_sdk -lagora_rtc_sdk"

# Build for x86-64 (Agora SDK is x86-64 only)
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -a -o /go/bin/server /server/cmd/video_conferencing

# Second step to build image with required libraries
FROM --platform=linux/amd64 ubuntu:22.04

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy built binary
COPY --from=build-env /go/bin/server /go/bin/server
COPY --from=build-env /server/config.json config.json
COPY --from=build-env /server/migrations migrations

# Copy Agora SDK libraries
COPY agora_sdk/*.so /usr/local/lib/

# Set library path
ENV LD_LIBRARY_PATH=/usr/local/lib:$LD_LIBRARY_PATH

ENTRYPOINT ["/go/bin/server"]
