const { spawn } = require('child_process');
const puppeteer = require('puppeteer');
const Redis = require('ioredis');
const path = require('path');
const fs = require('fs');

function resolveOutputPath(roomSlug, action, customUrl = '') {
  if (action === 'START_RTMP' && customUrl) {
    return customUrl;
  }
  const rawDir = process.env.RECORDINGS_DIR || path.join(__dirname, 'recordings');
  const dir = rawDir.replace(/\\/g, '/');
  if (fs && typeof fs.mkdirSync === 'function' && !fs.existsSync(dir)) {
    try {
      fs.mkdirSync(dir, { recursive: true });
    } catch (e) {}
  }
  if (customUrl && !customUrl.startsWith('rtmp://') && !customUrl.startsWith('rtmps://')) {
    return path.join(dir, customUrl).replace(/\\/g, '/');
  }
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
  return `${dir}/${roomSlug}_${timestamp}.mp4`;
}


function buildFFmpegArgs(roomSlug, outputUrl = 'output.mp4', options = {}) {
  const display = process.env.DISPLAY || ':99';
  // audioSource picks the ffmpeg audio input: 'pulse' captures real page
  // audio from the PulseAudio null-sink Chromium's output is routed to
  // (see start.sh), 'alsa' targets a hardware/ALSA default device, and
  // 'dummy' produces silence (used by tests, and as a last-resort fallback
  // if PulseAudio isn't available). useDummyAudio is kept for backward
  // compatibility with existing callers/tests.
  const audioSource = options.audioSource || (options.useDummyAudio ? 'dummy' : 'pulse');
  const pulseSink = process.env.PULSE_SINK || 'egress_sink';

  let audioInputArgs;
  if (audioSource === 'dummy') {
    audioInputArgs = ['-thread_queue_size', '1024', '-f', 'lavfi', '-i', 'anullsrc=channel_layout=stereo:sample_rate=44100'];
  } else if (audioSource === 'alsa') {
    audioInputArgs = ['-thread_queue_size', '1024', '-f', 'alsa', '-i', 'default'];
  } else {
    audioInputArgs = ['-thread_queue_size', '1024', '-f', 'pulse', '-i', `${pulseSink}.monitor`];
  }

  const isRTMP = outputUrl.startsWith('rtmp://') || outputUrl.startsWith('rtmps://');
  const formatArgs = isRTMP ? ['-f', 'flv'] : [];

  return [
    '-y',
    '-thread_queue_size', '1024',
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

// waitForEgressReady polls the page for window.__egressReady (set by the
// frontend's App.tsx once a real remote track has rendered, or after its
// own bounded grace timeout if the room is genuinely still empty — see
// App.tsx) instead of trusting Puppeteer's page.goto({waitUntil:
// 'networkidle2'}) to mean "ready to record". networkidle2 only tracks
// HTTP requests settling; it resolves as soon as the page's initial JS/CSS
// quiet down, which is well before the signaling WebSocket has joined the
// room, exchanged SDP, and rendered a single remote track — so without
// this, FFmpeg's x11grab starts capturing the empty lobby and stays behind
// that curve for however long the real handshake takes.
//
// page only needs an `evaluate` method (duck-typed against Puppeteer's
// Page), so this is directly unit-testable with a trivial mock instead of
// a real browser — per TESTING_STRATEGY.md, never a real Puppeteer/FFmpeg
// process in a unit test. Returns false (not a throw) on timeout, since a
// frontend bug here should degrade to "start recording anyway" rather than
// block a recording indefinitely.
async function waitForEgressReady(page, { timeoutMs = 15000, pollIntervalMs = 250 } = {}) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    let ready = false;
    try {
      ready = await page.evaluate(() => {
        if (typeof window === 'undefined') return false;
        if (window.__egressReady) return true;
        const videos = Array.from(document.querySelectorAll('video'));
        const remoteVideos = videos.filter((v) => !v.muted && v.readyState >= 2 && v.videoWidth > 0);
        return remoteVideos.length > 0;
      });
    } catch (e) {
      // page.evaluate can throw if the page navigated or closed mid-poll —
      // treat as not-ready-yet rather than crashing the recording start.
    }
    if (ready) return true;
    if (Date.now() >= deadline) return false;
    await new Promise((resolve) => setTimeout(resolve, pollIntervalMs));
  }
}

class EgressWorker {
  constructor({
    redisClient,
    redisPubClient,
    onCommand,
    logStderr,
    egressReadyTimeoutMs = 15000,
    egressReadyPollMs = 250,
  } = {}) {
    this.egressReadyTimeoutMs = egressReadyTimeoutMs;
    this.egressReadyPollMs = egressReadyPollMs;
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
    this.recordingProcess = null;
    this.rtmpProcess = null;
    this.browser = null;
    this.page = null;
    this.autoStopTimer = null;
    this.activeRoom = null;
    this.isStopping = false;
  }

  get ffmpegProcess() {
    return this.recordingProcess || this.rtmpProcess;
  }

  set ffmpegProcess(proc) {
    this.recordingProcess = proc;
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

    if (action === 'START_RECORDING') {
      const outputUrl = resolveOutputPath(roomSlug, action, command.url);
      await this.startCapture(roomSlug, 'recording', outputUrl);
    } else if (action === 'STOP_RECORDING') {
      this.stopCapture('recording');
    } else if (action === 'START_RTMP') {
      const outputUrl = resolveOutputPath(roomSlug, action, command.url);
      await this.startCapture(roomSlug, 'rtmp', outputUrl);
    } else if (action === 'STOP_RTMP') {
      this.stopCapture('rtmp');
    } else if (action === 'STOP_EGRESS') {
      console.log(`🛑 Stopping all Egress for room ${roomSlug}...`);
      this.stopGracefully('SIGINT');
    } else if (action === 'ROOM_EMPTY') {
      console.log(`⚠️ Room ${roomSlug} is empty. Scheduling auto-stop in 5 minutes...`);
      if (this.autoStopTimer) clearTimeout(this.autoStopTimer);
      this.autoStopTimer = setTimeout(() => {
        console.log(`⏰ 5 minutes empty room reached. Stopping Egress for ${roomSlug}`);
        this.stopGracefully('SIGINT');
      }, 5 * 60 * 1000);
    } else if (action === 'ROOM_ACTIVE') {
      if (this.autoStopTimer) {
        console.log(`👤 Participant active in room ${roomSlug}. Cancelling empty-room timer.`);
        clearTimeout(this.autoStopTimer);
        this.autoStopTimer = null;
      }
    }
  }

  async ensureBrowser(roomSlug) {
    if (this.browser && this.page && this.activeRoom === roomSlug) {
      return;
    }
    this.activeRoom = roomSlug;
    const frontendHost = process.env.FRONTEND_URL || 'http://frontend:3000';
    const targetUrl = `${frontendHost}?room=${roomSlug}&role=egress`;
    console.log(`🌐 Launching Puppeteer browser on DISPLAY ${process.env.DISPLAY || ':99'} targeting ${targetUrl}`);

    try {
      this.browser = await puppeteer.launch({
        executablePath: process.env.PUPPETEER_EXECUTABLE_PATH || undefined,
        headless: false,
        ignoreDefaultArgs: ['--enable-automation'],
        args: [
          '--no-sandbox',
          '--disable-setuid-sandbox',
          '--disable-dev-shm-usage',
          '--disable-infobars',
          `--unsafely-treat-insecure-origin-as-secure=${frontendHost}`,
          '--autoplay-policy=no-user-gesture-required',
          '--window-size=1920,1080',
          '--window-position=0,0',
          '--kiosk',
          `--display=${process.env.DISPLAY || ':99'}`,
        ],
        defaultViewport: { width: 1920, height: 1080 },
      });

      this.page = await this.browser.newPage();
      let pageLoaded = true;
      await this.page.goto(targetUrl, { waitUntil: 'networkidle2' }).catch(() => {
        pageLoaded = false;
        console.warn(`Could not reach ${targetUrl}, rendering browser viewport on :99`);
      });

      if (pageLoaded) {
        console.log('⏳ Waiting for egress page readiness signal before starting FFmpeg...');
        const ready = await waitForEgressReady(this.page, {
          timeoutMs: this.egressReadyTimeoutMs,
          pollIntervalMs: this.egressReadyPollMs,
        }).catch(() => false);
        if (ready) {
          console.log('✅ Egress page signaled ready — starting capture');
        } else {
          console.warn(`⚠️ Egress page did not signal readiness within ${this.egressReadyTimeoutMs}ms — starting capture anyway`);
        }
      }
    } catch (err) {
      console.warn('Puppeteer launch skipped or failed, proceeding with screen capture:', err.message);
    }
  }

  async startCapture(roomSlug, mode, outputUrl) {
    if (mode === 'recording' && this.recordingProcess) {
      console.log('Recording is already running for room', roomSlug);
      return;
    }
    if (mode === 'rtmp' && this.rtmpProcess) {
      console.log('RTMP stream is already running for room', roomSlug);
      return;
    }

    await this.ensureBrowser(roomSlug);

    const args = buildFFmpegArgs(roomSlug, outputUrl, { audioSource: 'pulse' });
    console.log(`🎥 Launching FFmpeg (${mode}) for room ${roomSlug} -> ${outputUrl}`);

    const proc = spawn('ffmpeg', args);

    if (mode === 'recording') {
      this.recordingProcess = proc;
    } else {
      this.rtmpProcess = proc;
    }

    proc.stdout?.on('data', (data) => {
      console.log(`[FFmpeg ${mode} STDOUT] ${data}`);
    });

    proc.stderr?.on('data', (data) => {
      const msg = data.toString();
      if (this.logStderr) {
        this.logStderr(msg);
      } else {
        console.error(`[FFmpeg ${mode} STDERR] ${msg}`);
      }
    });

    proc.on('close', (code, signal) => {
      const isGraceful = code === 0 || code === 255 || signal === 'SIGINT' || this.isStopping;
      if (isGraceful) {
        console.log(`✅ FFmpeg ${mode} finished gracefully (exit code ${code}, signal ${signal || 'SIGINT'}) - metadata headers finalized.`);
      } else {
        console.error(`❌ FFmpeg ${mode} process exited unexpectedly with code ${code}, signal ${signal}`);
      }

      if (mode === 'recording') {
        this.recordingProcess = null;
      } else {
        this.rtmpProcess = null;
      }

      if (!this.recordingProcess && !this.rtmpProcess) {
        this.closeBrowser();
      }

      if (this.redisPubClient && typeof this.redisPubClient.publish === 'function') {
        this.redisPubClient.publish('channel:egress_status', JSON.stringify({ status: 'completed', mode, room: roomSlug })).catch(() => {});
      }
    });
  }

  async startRecording(roomSlug, outputUrl) {
    return this.startCapture(roomSlug, 'recording', outputUrl);
  }

  stopCapture(mode, signal = 'SIGINT') {
    const proc = mode === 'recording' ? this.recordingProcess : this.rtmpProcess;
    if (proc) {
      console.log(`Sending ${signal} signal to FFmpeg (${mode}) for graceful closing...`);
      try {
        proc.kill(signal);
      } catch (e) {}
      if (mode === 'recording') {
        this.recordingProcess = null;
      } else {
        this.rtmpProcess = null;
      }
    }
    if (!this.recordingProcess && !this.rtmpProcess) {
      this.closeBrowser();
    }
  }

  closeBrowser() {
    if (this.autoStopTimer) {
      clearTimeout(this.autoStopTimer);
      this.autoStopTimer = null;
    }
    if (this.browser) {
      this.browser.close().catch(() => {});
      this.browser = null;
      this.page = null;
    }
  }

  stopGracefully(signal = 'SIGINT') {
    this.isStopping = true;
    if (this.recordingProcess) {
      console.log(`Sending ${signal} signal to FFmpeg recording for graceful header closing...`);
      try {
        this.recordingProcess.kill(signal);
      } catch (e) {}
      this.recordingProcess = null;
    }
    if (this.rtmpProcess) {
      console.log(`Sending ${signal} signal to FFmpeg rtmp for graceful header closing...`);
      try {
        this.rtmpProcess.kill(signal);
      } catch (e) {}
      this.rtmpProcess = null;
    }

    this.closeBrowser();
    this.isStopping = false;
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

module.exports = { EgressWorker, buildFFmpegArgs, resolveOutputPath, waitForEgressReady };

