# -------- build stage --------
FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/llm-proxy ./cmd/proxy

# -------- runtime stage --------
FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=builder /out/llm-proxy /llm-proxy

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/llm-proxy"]
