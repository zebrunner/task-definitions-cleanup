FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/cleanup ./cmd/cleanup
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/delete-task-definitions ./cmd/cleanup

FROM alpine:latest
WORKDIR /app

RUN adduser -D -u 10001 appuser
COPY --from=builder /out/delete-task-definitions /app/delete-task-definitions
USER appuser

ENTRYPOINT ["/app/delete-task-definitions"]
