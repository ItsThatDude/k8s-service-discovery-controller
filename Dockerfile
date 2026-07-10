# Stage 1: Build the Go binary
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY main.go main.go
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o build/server main.go

FROM gcr.io/distroless/static-debian12:latest

WORKDIR /

COPY --from=builder /app/build/server /server

USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/server"]