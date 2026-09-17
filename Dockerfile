# Cross-compiles on the build machine's own platform, so multi-arch images don't build under emulation.
FROM --platform=$BUILDPLATFORM golang:1-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=${VERSION#v}" -o /emulsion .

FROM alpine:3
RUN apk add --no-cache git exiftool ca-certificates tzdata
COPY --from=build /emulsion /usr/local/bin/emulsion
VOLUME ["/data"]
EXPOSE 8080
# Mount your photo share at /photos (becomes the first library) and set EMULSION_PASSWORD.
ENTRYPOINT ["emulsion", "-headless", "-data", "/data", "-addr", "0.0.0.0:8080", "-library", "/photos"]
