# **Naka Meet: Docker & Architecture Configuration**

## **1\. Overview**

This document contains the complete orchestration setup for the Naka Meet platform. It utilizes a microservices monorepo architecture, splitting the high-performance Golang WebRTC SFU from the resource-heavy Node.js Egress Worker, united via Redis.

## **2\. docker-compose.yml (Root Directory)**

This file orchestrates the entire local environment, ensuring correct networking and dependency startup sequences.

`services:`  
  `redis:`  
    `image: redis:7-alpine`  
    `container_name: naka-redis`  
    `ports:`  
      `- "6379:6379"`  
    `networks:`  
      `- naka_network`  
    `healthcheck:`  
      `test: ["CMD", "redis-cli", "ping"]`  
      `interval: 5s`  
      `timeout: 3s`  
      `retries: 5`

  `sfu-backend:`  
    `build:`  
      `context: ./apps/sfu`  
      `dockerfile: Dockerfile`  
    `container_name: naka-sfu`  
    `ports:`  
      `# HTTP & WebSocket Signaling`  
      `- "8080:8080"`  
      `# WebRTC UDP Ports (Meskipun di Docker, kita harus expose range-nya)`  
      `- "50000-50050:50000-50050/udp"`  
    `environment:`  
      `- REDIS_URL=redis:6379`  
      `- WEBRTC_UDP_PORT_MIN=50000`  
      `- WEBRTC_UDP_PORT_MAX=50050`  
    `depends_on:`  
      `redis:`  
        `condition: service_healthy`  
    `networks:`  
      `- naka_network`  
    `# Penting untuk WebRTC agar tidak terjebak NAT rumit di lokal`  
    `# network_mode: "host" # (Bisa diaktifkan jika simulasi ICE bermasalah di Linux)`

  `egress-worker:`  
    `build:`  
      `context: ./apps/egress`  
      `dockerfile: Dockerfile`  
    `container_name: naka-egress`  
    `environment:`  
      `- REDIS_URL=redis:6379`  
      `# Mode Xvfb (Virtual Screen)`  
      `- DISPLAY=:99`   
    `depends_on:`  
      `redis:`  
        `condition: service_healthy`  
    `networks:`  
      `- naka_network`  
    `# Resource limit untuk mencegah Egress menumbangkan OS`  
    `deploy:`  
      `resources:`  
        `limits:`  
          `cpus: '1.5'`  
          `memory: 1G`

`networks:`  
  `naka_network:`  
    `driver: bridge`

## **3\. apps/sfu/Dockerfile (Golang SFU)**

This is a highly optimized, two-stage Dockerfile for the Golang backend. It compiles the binary statically, resulting in a microscopic final image (usually \< 30MB) based on Alpine.

`# -------------------------`  
`# Stage 1: Build the binary`  
`# -------------------------`  
`FROM golang:1.26-alpine AS builder`

`# Install SSL CA certificates (penting jika SFU perlu memanggil external API over HTTPS)`  
`RUN apk --no-cache add ca-certificates`

`WORKDIR /app`

`# Cache go modules`  
`COPY go.mod go.sum ./`  
`RUN go mod download`

`COPY . .`

`# Build static binary (CGO_ENABLED=0 penting untuk Alpine)`  
`RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o sfu-server ./main.go`

`# -------------------------`  
`# Stage 2: Minimal Runtime`  
`# -------------------------`  
`FROM alpine:latest`

`# Bawa sertifikat SSL dari stage builder`  
`COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/`

`WORKDIR /root/`

`# Copy binary dari stage builder`  
`COPY --from=builder /app/sfu-server .`

`EXPOSE 8080`  
`# Expose default WebRTC ports UDP (Pion)`  
`EXPOSE 50000-50050/udp` 

`CMD ["./sfu-server"]`

## **4\. apps/egress/Dockerfile (Node.js \+ FFmpeg Worker)**

This is the heavy-lifting container. It requires a full OS environment (Debian/Ubuntu-based) to run a headless Chromium browser using Puppeteer and Xvfb (virtual display), alongside FFmpeg for media encoding.

`FROM node:24-bookworm-slim`

`# Menghindari prompt interaktif saat apt-get install`  
`ENV DEBIAN_FRONTEND=noninteractive`

`# Install dependencies: Xvfb (virtual display), FFmpeg, dan librari sistem untuk Chromium`  
`RUN apt-get update && apt-get install -y \`  
    `xvfb \`  
    `ffmpeg \`  
    `chromium \`  
    `libxss1 \`  
    `libasound2 \`  
    `fonts-liberation \`  
    `libappindicator3-1 \`  
    `xdg-utils \`  
    `--no-install-recommends \`  
    `&& rm -rf /var/lib/apt/lists/*`

`# Skip download Chromium internal dari Puppeteer, gunakan binary Chromium bawaan Debian`  
`ENV PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true`  
`ENV PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium`

`WORKDIR /usr/src/app`

`COPY package*.json ./`

`RUN npm install`

`COPY . .`

`# Script entrypoint khusus untuk menyalakan Xvfb sebelum menjalankan Node.js`  
`# Xvfb berjalan di background pada port virtual :99 dengan resolusi 1920x1080`  
`RUN echo '#!/bin/bash\n\`  
`Xvfb :99 -screen 0 1920x1080x24 -ac & \n\`  
`sleep 2\n\`  
`node worker.js' > /usr/src/app/start.sh`

`RUN chmod +x /usr/src/app/start.sh`

`CMD ["/usr/src/app/start.sh"]`

## **5\. Architectural Notes & Gotchas**

> * **UDP Port Mapping:** WebRTC requires a wide range of UDP ports to establish connections (Trickle ICE). In the Golang Dockerfile and Compose, we explicitly map 50000-50050. You **must** configure Pion in your Go code to use this exact port range.  
> * **Xvfb Importance:** Chromium cannot launch without an attached display. Since servers lack monitors, Xvfb creates a fake "virtual monitor" (DISPLAY=:99) in the Egress container, allowing Puppeteer to render the React UI just like it would on a physical laptop.  
> * **Resource Limits:** The Compose file aggressively limits the Egress Worker to 1.5 CPUs and 1GB RAM. If FFmpeg misbehaves, Docker will throttle or restart only that container, protecting the main Golang SFU node from failing.