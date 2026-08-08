FROM golang:1.25.7-alpine3.22 AS build

ENV GOTOOLCHAIN=go1.26.5
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gradex-api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gradex-worker ./cmd/worker \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gradex-migrate ./cmd/migrate

FROM build AS proof-build

RUN CGO_ENABLED=0 go test -c -trimpath -o /out/gradex-e2e-seed ./cmd/e2e-seed \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gradex-storage-fixture ./cmd/storage-fixture

FROM alpine:3.22.5 AS runtime-base

ARG GRADEX_REVISION=unknown
LABEL org.opencontainers.image.revision=$GRADEX_REVISION

RUN apk add --no-cache ca-certificates ffmpeg tzdata \
    && addgroup -S gradex \
    && adduser -S -G gradex gradex

WORKDIR /app

USER gradex

FROM runtime-base AS proof

ARG GRADEX_REVISION=unknown
LABEL org.opencontainers.image.revision=$GRADEX_REVISION

COPY --from=proof-build /out/gradex-e2e-seed /usr/local/bin/gradex-e2e-seed
COPY --from=proof-build /out/gradex-storage-fixture /usr/local/bin/gradex-storage-fixture
COPY --from=build /src/internal/db/migrations ./internal/db/migrations

ENTRYPOINT ["gradex-e2e-seed"]

FROM runtime-base AS runtime

ARG GRADEX_REVISION=unknown
LABEL org.opencontainers.image.revision=$GRADEX_REVISION

COPY --from=build /out/gradex-api /usr/local/bin/gradex-api
COPY --from=build /out/gradex-worker /usr/local/bin/gradex-worker
COPY --from=build /out/gradex-migrate /usr/local/bin/gradex-migrate
COPY --from=build /src/internal/db/migrations ./internal/db/migrations

EXPOSE 8080

CMD ["gradex-api"]
