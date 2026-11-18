# Copyright 2026 The Tanpopo Authors.
# Licensed under the Apache License, Version 2.0.

FROM node:22.18.0-alpine3.22@sha256:1b2479dd35a99687d6638f5976fd235e26c5b37e8122f786fcd5fe231d63de5b AS console-build
WORKDIR /console
COPY web/console/package.json web/console/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web/console/ ./
COPY openapi/ /openapi/
RUN npm run build

FROM golang:1.26.11-alpine@sha256:ecb67f987aa1a57794c91d19ade7ae0209f2f0f5496aaef9e6ae85ddf92d02ab AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf internal/webui/dist && mkdir -p internal/webui/dist
COPY --from=console-build /console/dist/ ./internal/webui/dist/
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
