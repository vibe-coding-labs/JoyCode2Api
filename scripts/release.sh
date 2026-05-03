#!/usr/bin/env bash
set -euo pipefail

# ── JoyCodeProxy Release Script ──────────────────────────────────────
# Usage: ./scripts/release.sh <version>
# Example: ./scripts/release.sh v0.3.0
#
# Prerequisites:
#   - Go 1.25+
#   - Node.js 20+
#   - Docker (for Linux builds)
#   - GitHub CLI (gh) authenticated
#
# What it does:
#   1. Build frontend (React → static assets)
#   2. Update version.go with the given version
#   3. Build darwin-arm64 binary natively
#   4. Build linux-amd64 binary via Docker
#   5. Create git tag and GitHub Release with all binaries

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
VERSION_FILE="$ROOT_DIR/cmd/JoyCodeProxy/version.go"
BINARY_NAME="JoyCodeProxy"

# ── Validate input ───────────────────────────────────────────────────

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v0.3.0"
    exit 1
fi

# Strip leading 'v' for the Go variable, keep it for git tag
VERSION_NUM="${VERSION#v}"

echo "=========================================="
echo "  JoyCodeProxy Release: $VERSION"
echo "=========================================="

# ── Step 1: Build frontend ───────────────────────────────────────────

echo ""
echo "[1/6] Building frontend..."
cd "$ROOT_DIR/web"
npm install --silent
npm run build

if [[ ! -f "$ROOT_DIR/cmd/JoyCodeProxy/static/index.html" ]]; then
    echo "ERROR: Frontend build failed - index.html not found"
    exit 1
fi
echo "  Frontend built successfully"

# ── Step 2: Update version.go ────────────────────────────────────────

echo ""
echo "[2/6] Updating version to $VERSION_NUM..."
cd "$ROOT_DIR"

# Use sed to update the Version variable
if [[ "$(uname)" == "Darwin" ]]; then
    sed -i '' "s/var Version = \".*\"/var Version = \"$VERSION_NUM\"/" "$VERSION_FILE"
else
    sed -i "s/var Version = \".*\"/var Version = \"$VERSION_NUM\"/" "$VERSION_FILE"
fi

echo "  Version updated to $VERSION_NUM"

# ── Step 3: Clean and prepare dist ───────────────────────────────────

echo ""
echo "[3/6] Preparing dist directory..."
rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

# ── Step 4: Build darwin-arm64 ───────────────────────────────────────

echo ""
echo "[4/6] Building darwin-arm64..."
cd "$ROOT_DIR"

CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
    go build -trimpath -ldflags "-s -w" \
    -o "$DIST_DIR/${BINARY_NAME}-darwin-arm64" \
    ./cmd/JoyCodeProxy/

echo "  Built: ${BINARY_NAME}-darwin-arm64 ($(du -h "$DIST_DIR/${BINARY_NAME}-darwin-arm64" | cut -f1))"

# Quick smoke test - verify binary runs
"$DIST_DIR/${BINARY_NAME}-darwin-arm64" version
echo "  Smoke test passed"

# ── Step 5: Build linux-amd64 via Docker ─────────────────────────────

echo ""
echo "[5/6] Building linux-amd64 via Docker..."

# Create a temporary Dockerfile for extracting the binary
cat > "$ROOT_DIR/Dockerfile.build" <<'DOCKERFILE'
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" \
    -o /JoyCodeProxy ./cmd/JoyCodeProxy/

FROM alpine:3.21
COPY --from=builder /JoyCodeProxy /JoyCodeProxy
DOCKERFILE

docker build --platform linux/amd64 \
    --build-arg VERSION="$VERSION_NUM" \
    -f "$ROOT_DIR/Dockerfile.build" \
    -t joycode-build:"$VERSION_NUM" \
    "$ROOT_DIR"

# Extract binary from Docker image
CONTAINER_ID=$(docker create "joycode-build:$VERSION_NUM")
docker cp "$CONTAINER_ID:/JoyCodeProxy" "$DIST_DIR/${BINARY_NAME}-linux-amd64"
docker rm "$CONTAINER_ID" > /dev/null

# Clean up
rm "$ROOT_DIR/Dockerfile.build"

# Verify it's a Linux binary
file "$DIST_DIR/${BINARY_NAME}-linux-amd64" | grep -q "ELF" && \
    echo "  Built: ${BINARY_NAME}-linux-amd64 ($(du -h "$DIST_DIR/${BINARY_NAME}-linux-amd64" | cut -f1))" || \
    { echo "ERROR: Linux binary verification failed"; exit 1; }

# ── Step 6: Commit, tag, and create GitHub Release ───────────────────

echo ""
echo "[6/6] Creating GitHub Release..."

# Commit version change
git add "$VERSION_FILE" "$ROOT_DIR/Dockerfile" "$ROOT_DIR/cmd/JoyCodeProxy/static/"
git commit -m "release: $VERSION" || echo "  Nothing new to commit"

# Create git tag
git tag -a "$VERSION" -m "Release $VERSION"

# Push commit and tag
git push origin main
git push origin "$VERSION"

# Generate checksums
cd "$DIST_DIR"
shasum -a 256 "${BINARY_NAME}-"* > checksums-sha256.txt
cat checksums-sha256.txt

# Create GitHub Release
gh release create "$VERSION" \
    "$DIST_DIR/${BINARY_NAME}-darwin-arm64" \
    "$DIST_DIR/${BINARY_NAME}-linux-amd64" \
    "$DIST_DIR/checksums-sha256.txt" \
    --title "JoyCodeProxy $VERSION" \
    --notes "## JoyCodeProxy $VERSION

### Downloads
- \`JoyCodeProxy-darwin-arm64\` — macOS (Apple Silicon)
- \`JoyCodeProxy-linux-amd64\` — Linux (x86_64)

### Quick Start
\`\`\`bash
# macOS
chmod +x JoyCodeProxy-darwin-arm64
./JoyCodeProxy-darwin-arm64 serve

# Linux
chmod +x JoyCodeProxy-linux-amd64
./JoyCodeProxy-linux-amd64 serve
\`\`\`

### Verify checksum
\`\`\`bash
shasum -a 256 -c checksums-sha256.txt
\`\`\`"

echo ""
echo "=========================================="
echo "  Release $VERSION published successfully!"
echo "=========================================="

# Clean up Docker image
docker rmi "joycode-build:$VERSION_NUM" > /dev/null 2>&1 || true
