FROM golang:1.22-alpine AS build
WORKDIR /src
COPY main.go .
RUN go build -o /out/mock-openai main.go

FROM alpine:3.20
COPY --from=build /out/mock-openai /mock-openai
EXPOSE 8080
ENTRYPOINT ["/mock-openai"]
