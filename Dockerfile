# One image: the Go control plane serving the built dashboard, plus the
# API gateway. Built for linux/amd64 and linux/arm64: the dashboard and Go
# compile on the build machine's own platform, Go cross-compiles to the
# target, so no stage runs under emulation.

FROM --platform=$BUILDPLATFORM node:20-alpine AS dashboard
WORKDIR /src
COPY package.json package-lock.json ./
COPY packages/design-system/package.json packages/design-system/
COPY dashboard/package.json dashboard/
RUN npm ci
COPY packages/design-system packages/design-system
COPY dashboard dashboard
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS api
WORKDIR /src
COPY control-plane/go.mod control-plane/go.sum ./
RUN go mod download
COPY control-plane .
ARG VERSION=dev
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "-s -w -X main.version=${VERSION}" -o /out/flobase-api ./cmd/flobase-api \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "-s -w" -o /out/flobase-gateway ./cmd/flobase-gateway

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=api /out/flobase-api /out/flobase-gateway /usr/local/bin/
COPY --from=dashboard /src/dashboard/dist /srv/dashboard
ENV FLOBASE_DASHBOARD_DIR=/srv/dashboard FLOBASE_ADDR=:8080
EXPOSE 8080
LABEL org.opencontainers.image.source="https://github.com/flomo-abel-231/flobase" \
      org.opencontainers.image.description="FloBase control plane, dashboard and API gateway" \
      org.opencontainers.image.licenses="Apache-2.0"
ENTRYPOINT ["flobase-api"]
