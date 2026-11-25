FROM golang:1.27 AS builder

WORKDIR /app

COPY . .

ARG GIT_TOKEN

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go env -w GOPRIVATE=github.com/YOLOTECHNOLOGY \
 && go env -w GONOSUMDB=github.com/YOLOTECHNOLOGY && go env -w GONOPROXY=github.com/YOLOTECHNOLOGY \
 && git config --global url."https://cicd:${GIT_TOKEN}@github.com".insteadOf "https://github.com" \
 && GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o main ./cmd

FROM debian:12
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y curl tzdata ca-certificates && \
    rm -rf /var/lib/apt/lists/*

ENV TZ "Asia/Singapore"
WORKDIR /app
COPY --from=builder /app/main .
COPY --from=builder /app/development_config.yaml .

EXPOSE 8000

CMD ["./main", "serve"]
