# -------- build stage --------
FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/llm-collector .

# -------- runtime stage --------
FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=builder /out/llm-collector /llm-collector

EXPOSE 8081
USER nonroot:nonroot

ENTRYPOINT ["/llm-collector"]
