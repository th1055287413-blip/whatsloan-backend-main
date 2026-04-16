FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-s -w \
      -X whatsapp_golang/internal/config.Version=${VERSION} \
      -X whatsapp_golang/internal/config.GitCommit=${GIT_COMMIT} \
      -X whatsapp_golang/internal/config.BuildTime=${BUILD_TIME}" \
    -o /api ./cmd/api

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /api /app/api

WORKDIR /app
ENTRYPOINT ["/app/api"]
