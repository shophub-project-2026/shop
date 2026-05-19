FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Build only the root main package. Using `./...` here breaks once the
# module has multiple packages, because `-o /app` then has to write more
# than one output to a non-directory path.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags='-s -w' -o /app .

FROM alpine:3.18
RUN apk add --no-cache ca-certificates
COPY --from=builder /app /app
USER 65532:65532
ENTRYPOINT ["/app"]
