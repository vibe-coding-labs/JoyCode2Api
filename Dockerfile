FROM golang:1.22-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -ldflags "-s -w" -o /JoyCodeProxy ./cmd/JoyCodeProxy

FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /JoyCodeProxy /usr/local/bin/JoyCodeProxy

EXPOSE 34891

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:34891/health || exit 1

ENTRYPOINT ["JoyCodeProxy"]
CMD ["serve"]
