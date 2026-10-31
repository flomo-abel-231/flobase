# One image: the Go control plane serving the built dashboard.

FROM node:20-alpine AS dashboard
WORKDIR /src
COPY package.json package-lock.json ./
COPY packages/design-system/package.json packages/design-system/
COPY dashboard/package.json dashboard/
RUN npm ci
COPY packages/design-system packages/design-system
COPY dashboard dashboard
RUN npm run build

FROM golang:1.24-alpine AS api
WORKDIR /src
COPY control-plane/go.mod control-plane/go.sum ./
RUN go mod download
COPY control-plane .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /flobase-api ./cmd/flobase-api \
 && CGO_ENABLED=0 go build -ldflags "-s -w" -o /flobase-gateway ./cmd/flobase-gateway

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=api /flobase-api /flobase-gateway /usr/local/bin/
COPY --from=dashboard /src/dashboard/dist /srv/dashboard
ENV FLOBASE_DASHBOARD_DIR=/srv/dashboard FLOBASE_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["flobase-api"]
