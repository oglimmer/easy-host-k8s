FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 appgroup \
    && adduser -u 10001 -G appgroup -D -h /app appuser
COPY --from=builder /server /server
COPY templates/ /templates/
COPY static/ /static/
USER 10001
EXPOSE 8080
ENTRYPOINT ["/server"]
