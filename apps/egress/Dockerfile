FROM node:24-bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    xvfb \
    ffmpeg \
    chromium \
    libxss1 \
    libasound2 \
    fonts-liberation \
    libappindicator3-1 \
    xdg-utils \
    --no-install-recommends \
    && rm -rf /var/lib/apt/lists/*

ENV PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true
ENV PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium

WORKDIR /usr/src/app

COPY package*.json ./

RUN npm install

COPY . .

RUN mkdir -p recordings


RUN echo '#!/bin/bash\n\
Xvfb :99 -screen 0 1920x1080x24 -ac & \n\
sleep 2\n\
node worker.js' > /usr/src/app/start.sh

RUN chmod +x /usr/src/app/start.sh

CMD ["/usr/src/app/start.sh"]
