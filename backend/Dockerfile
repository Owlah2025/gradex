FROM golang:1.25.7-alpine3.22 AS build

ENV GOTOOLCHAIN=go1.26.5
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gradex-api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gradex-worker ./cmd/worker \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gradex-migrate ./cmd/migrate

FROM alpine:3.22.5

RUN apk add --no-cache ca-certificates ffmpeg tzdata \
    && addgroup -S gradex \
    && adduser -S -G gradex gradex

WORKDIR /app

COPY --from=build /out/gradex-api /usr/local/bin/gradex-api
COPY --from=build /out/gradex-worker /usr/local/bin/gradex-worker
COPY --from=build /out/gradex-migrate /usr/local/bin/gradex-migrate
COPY --from=build /src/internal/db/migrations ./internal/db/migrations

USER gradex
EXPOSE 8080

CMD ["gradex-api"]
