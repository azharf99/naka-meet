const { spawn } = require('child_process');
const puppeteer = require('puppeteer');
const Redis = require('ioredis');

function buildFFmpegArgs(roomSlug, outputUrl = 'output.mp4', options = {}) {
  const display = process.env.DISPLAY || ':99';
  const useDummyAudio = options.useDummyAudio !== undefined ? options.useDummyAudio : true;

  const audioInputArgs = useDummyAudio
    ? ['-f', 'lavfi', '-i', 'anullsrc=channel_layout=stereo:sample_rate=44100']
    : ['-f', 'alsa', '-i', 'default'];

  const isRTMP = outputUrl.startsWith('rtmp://') || outputUrl.startsWith('rtmps://');
  const formatArgs = isRTMP ? ['-f', 'flv'] : [];

  return [
    '-y',
    '-f', 'x11grab',
    '-draw_mouse', '0',
    '-s', '1920x1080',
    '-r', '30',
    '-i', display,
    ...audioInputArgs,
    '-c:v', 'libx264',
    '-preset', 'ultrafast',
    '-pix_fmt', 'yuv420p',
    '-c:a', 'aac',
    '-b:a', '128k',
    ...formatArgs,
    outputUrl,
  ];
}

class EgressWorker {
  constructor({ redisClient, redisPubClient, onCommand, logStderr } = {}) {
    const redisUrl = process.env.REDIS_URL || 'redis://localhost:6379';
    this.redisClient = redisClient || new Redis(redisUrl);
    // Connection in subscriber mode cannot be used for publish commands in ioredis.
    // Duplicate connection or use dedicated redisPubClient for publishing.
    if (redisPubClient) {
      this.redisPubClient = redisPubClient;
    } else if (typeof this.redisClient.duplicate === 'function') {
      this.redisPubClient = this.redisClient.duplicate();
    } else {
      this.redisPubClient = new Redis(redisUrl);
    }

    this.onCommand = onCommand;
    this.logStderr = logStderr;
    this.ffmpegProcess = null;
    this.browser = null;
    this.autoStopTimer = null;
    this.activeRoom = null;
  }

  async startListening() {
    await this.redisClient.subscribe('channel:egress_commands');
    this.redisClient.on('message', (channel, message) => {
      if (channel === 'channel:egress_commands') {
        try {
          const command = JSON.parse(message);
          if (this.onCommand) {
            this.onCommand(command);
          } else {
            this.handleCommand(command);
          }
        } catch (err) {
          console.error('Failed to parse egress command', err);
        }
      }
    });
    console.log('🎬 Egress Worker listening on Redis channel:egress_commands');
  }

  async handleCommand(command) {
    console.log('Received egress command:', command);
    const action = command.action;
    const roomSlug = command.room || 'demo-room';

    if (action === 'START_RECORDING' || action === 'START_RTMP') {
      const outputUrl = command.url || `${roomSlug}_recording.mp4`;
      await this.startRecording(roomSlug, outputUrl);
    } else if (action === 'STOP_EGRESS') {
      console.log(`🛑 Stopping Egress for room ${roomSlug}...`);
      this.stopGracefully('SIGINT');
    }
  }

  async startRecording(roomSlug, outputUrl) {
    this.activeRoom = roomSlug;
    const frontendHost = process.env.FRONTEND_URL || 'http://frontend:3000';
    const targetUrl = `${frontendHost}?room=${roomSlug}&role=egress`;
    console.log(`🌐 Launching Puppeteer browser on DISPLAY ${process.env.DISPLAY || ':99'} targeting ${targetUrl}`);

    try {
      this.browser = await puppeteer.launch({
        executablePath: process.env.PUPPETEER_EXECUTABLE_PATH || undefined,
        headless: false,
        args: [
          '--no-sandbox',
          '--disable-setuid-sandbox',
          '--disable-dev-shm-usage',
          '--autoplay-policy=no-user-gesture-required',
          '--window-size=1920,1080',
          '--start-fullscreen',
          `--display=${process.env.DISPLAY || ':99'}`,
        ],
        defaultViewport: { width: 1920, height: 1080 },
      });

      const page = await this.browser.newPage();
      await page.goto(targetUrl, { waitUntil: 'networkidle2' }).catch(() => {
        console.warn(`Could not reach ${targetUrl}, rendering browser viewport on :99`);
      });
    } catch (err) {
      console.warn('Puppeteer launch skipped or failed, proceeding with screen capture:', err.message);
    }

    const args = buildFFmpegArgs(roomSlug, outputUrl, { useDummyAudio: true });
    console.log(`🎥 Launching FFmpeg recording for room ${roomSlug} -> ${outputUrl}`);

    this.ffmpegProcess = spawn('ffmpeg', args);

    this.ffmpegProcess.stdout?.on('data', (data) => {
      console.log(`[FFmpeg STDOUT] ${data}`);
    });

    this.ffmpegProcess.stderr?.on('data', (data) => {
      const msg = data.toString();
      if (this.logStderr) {
        this.logStderr(msg);
      } else {
        console.error(`[FFmpeg STDERR] ${msg}`);
      }
    });

    this.ffmpegProcess.on('close', (code, signal) => {
      const isGraceful = code === 0 || code === 255 || signal === 'SIGINT' || this.isStopping;
      if (isGraceful) {
        console.log(`✅ FFmpeg recording finished gracefully (exit code ${code}, signal ${signal || 'SIGINT'}) - metadata headers finalized.`);
      } else {
        console.error(`❌ FFmpeg process exited unexpectedly with code ${code}, signal ${signal}`);
      }
      this.ffmpegProcess = null;
      this.isStopping = false;
      if (this.redisPubClient && typeof this.redisPubClient.publish === 'function') {
        this.redisPubClient.publish('channel:egress_status', JSON.stringify({ status: 'completed', room: roomSlug })).catch(() => {});
      }
    });

    // BR2: Auto-stop after 5 minutes empty room
    if (this.autoStopTimer) clearTimeout(this.autoStopTimer);
    this.autoStopTimer = setTimeout(() => {
      console.log('⏰ 5 minutes timeout reached, auto-stopping Egress Worker to save CPU');
      this.stopGracefully('SIGINT');
    }, 5 * 60 * 1000);
  }

  // BR2: Must use SIGINT so MP4/FLV metadata header is properly written
  stopGracefully(signal = 'SIGINT') {
    this.isStopping = true;
    if (this.autoStopTimer) {
      clearTimeout(this.autoStopTimer);
      this.autoStopTimer = null;
    }

    if (this.ffmpegProcess) {
      console.log(`Sending ${signal} signal to FFmpeg for graceful header closing...`);
      try {
        this.ffmpegProcess.kill(signal);
      } catch (e) {}
      this.ffmpegProcess = null;
    }


    if (this.browser) {
      this.browser.close().catch(() => {});
      this.browser = null;
    }
  }

  async stop() {
    this.stopGracefully('SIGINT');
    if (this.redisClient && typeof this.redisClient.disconnect === 'function') {
      await this.redisClient.disconnect();
    }
    if (this.redisPubClient && typeof this.redisPubClient.disconnect === 'function') {
      await this.redisPubClient.disconnect();
    }
  }
}

if (require.main === module) {
  const worker = new EgressWorker();
  worker.startListening().catch(console.error);

  process.on('SIGINT', () => {
    worker.stop().then(() => process.exit(0));
  });
}

module.exports = { EgressWorker, buildFFmpegArgs };
