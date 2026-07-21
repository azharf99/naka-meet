# -------------------------
# Stage 1: Build binary
# -------------------------
FROM golang:1.26-alpine AS builder

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o sfu-server ./main.go

# -------------------------
# Stage 2: Minimal Runtime
# -------------------------
FROM alpine:latest

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /root/

COPY --from=builder /app/sfu-server .

EXPOSE 8080
EXPOSE 50000-50050/udp

CMD ["./sfu-server"]
