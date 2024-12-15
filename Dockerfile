FROM --platform=$BUILDPLATFORM golang:1.23-alpine as build

RUN apk --no-cache add build-base vips-dev

WORKDIR /proxy
COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN \
  --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,target=/root/go/pkg \
  GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=1 \
  go build -o ./goofy-image-proxy .

FROM alpine:3.21

RUN apk --no-cache add tini vips

COPY --from=build --chmod=0555 /proxy/goofy-image-proxy /usr/local/bin/goofy-image-proxy

ENTRYPOINT [ "tini", "--", "/usr/local/bin/goofy-image-proxy" ]
