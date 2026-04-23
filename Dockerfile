ARG GO_VERSION=1.26
ARG SERVICE=playback

FROM golang:${GO_VERSION}-alpine AS build
ARG SERVICE
WORKDIR /src

RUN apk add --no-cache git

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/app ./cmd/${SERVICE}

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
