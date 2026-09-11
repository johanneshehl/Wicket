# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/wicket . \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="Wicket" \
      org.opencontainers.image.description="Self-hosted login gate with single sign-on for Caddy (forward auth, 2FA, audit log)" \
      org.opencontainers.image.source="https://github.com/johanneshehl/wicket"
COPY --from=build /out/wicket /wicket
COPY --from=build --chown=65532:65532 /out/data /data
ENV WICKET_DATA=/data
VOLUME /data
EXPOSE 9091
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/wicket", "healthcheck"]
ENTRYPOINT ["/wicket"]
