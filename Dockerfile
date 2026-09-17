FROM golang:1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /emulsion .

FROM alpine:3
RUN apk add --no-cache git exiftool ca-certificates tzdata
COPY --from=build /emulsion /usr/local/bin/emulsion
VOLUME ["/data"]
EXPOSE 8080
# Mount your photo share at /photos (becomes the first library) and set EMULSION_PASSWORD.
ENTRYPOINT ["emulsion", "-headless", "-data", "/data", "-addr", "0.0.0.0:8080", "-library", "/photos"]
