#!/bin/bash
set -e

SERVICE_NAME="${SERVICE_NAME:-myapp}" # Default service name if not set
BUILD_CGO_ENABLED="${BUILD_CGO_ENABLED:-0}" # Default CGO disabled if not set

# Define target platforms (OS/ARCH/extension)
TARGETS=(
    "linux/amd64/"
    "windows/amd64/.exe"
    "darwin/amd64/"
    "darwin/arm64/"
)

# Ensure vendor directory is up-to-date
make vendor

for target in "${TARGETS[@]}"; do
    IFS='/' read -r os arch ext <<< "$target"
    output_name="${SERVICE_NAME}_${os}_${arch}${ext}"
    echo "Building for $os/$arch as $output_name"
    CGO_ENABLED="$BUILD_CGO_ENABLED" GOOS="$os" GOARCH="$arch" \
    go build -ldflags="-s -w" -a -o "$output_name" cmd/main/main.go
    echo "Build for $os/$arch ('$output_name') complete."
done

echo "Multiplatform build complete."