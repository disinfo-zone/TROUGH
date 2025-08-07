FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o trough .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/trough .
COPY --from=builder /build/static ./static
COPY --from=builder /build/db ./db
EXPOSE 8080
CMD ["./trough"]