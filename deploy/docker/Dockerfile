# Copyright 2026 The Tanpopo Authors.
# Licensed under the Apache License, Version 2.0.

FROM golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath \
    -ldflags="-s -w" -o /out/tanpopo ./cmd/tanpopo
RUN mkdir -p /out/data && chown 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
WORKDIR /
COPY --from=build --chown=65532:65532 /out/tanpopo /tanpopo
COPY --from=build --chown=65532:65532 /out/data /var/lib/tanpopo
ENV TANPOPO_DATA_DIR=/var/lib/tanpopo
VOLUME ["/var/lib/tanpopo"]
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/tanpopo"]
CMD ["serve", "--listen", "0.0.0.0:8080"]
