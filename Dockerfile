FROM --platform=$BUILDPLATFORM node:24.18.0-alpine3.24 AS builder

ARG VERSION=1.0.0
WORKDIR /build
COPY ./web/package*.json ./
RUN npm ci
COPY ./web .
RUN VITE_VERSION=${VERSION} npm run build


FROM --platform=$BUILDPLATFORM golang:1.26.10-alpine@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca AS builder2

ARG VERSION=1.0.0
ARG TARGETOS
ARG TARGETARCH
ENV GO111MODULE=on \
    CGO_ENABLED=0

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=builder /build/dist ./web/dist
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags "-s -w -X gpt-load/internal/version.Version=${VERSION}" -o gpt-load


FROM alpine:3.24@sha256:c6b073f8e9e58d881f6ffba6ebf3b9ebd60b9e7d6e8337018c0e68bb5c356873

WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata \
    && update-ca-certificates

COPY --from=builder2 /build/gpt-load .
EXPOSE 3001
ENTRYPOINT ["/app/gpt-load"]
