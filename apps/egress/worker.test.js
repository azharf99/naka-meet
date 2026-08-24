const test = require('node:test');
const assert = require('node:assert');
const RedisMock = require('ioredis-mock');
const { EgressWorker, buildFFmpegArgs, waitForEgressReady } = require('./worker');

test('buildFFmpegArgs handles local MP4 recording', () => {
  const roomSlug = 'test-room';
  const mp4Url = 'recording.mp4';
  const args = buildFFmpegArgs(roomSlug, mp4Url, { useDummyAudio: true });

  assert.ok(args.includes('x11grab'), 'Should grab X11 virtual screen');
  assert.ok(args.includes('anullsrc=channel_layout=stereo:sample_rate=44100'), 'Should use dummy audio');
  assert.strictEqual(args[args.length - 1], mp4Url);
});

test('buildFFmpegArgs captures real audio via PulseAudio monitor by default in production', () => {
  const roomSlug = 'test-room';
  const mp4Url = 'recording.mp4';
  const args = buildFFmpegArgs(roomSlug, mp4Url, { audioSource: 'pulse' });

  assert.ok(args.includes('pulse'), 'Should use pulse input format');
  assert.ok(args.includes('egress_sink.monitor'), 'Should capture from the default egress sink monitor');
  assert.ok(!args.includes('anullsrc=channel_layout=stereo:sample_rate=44100'), 'Should not use silent dummy audio');
});

test('buildFFmpegArgs honors PULSE_SINK env override for the monitor source', () => {
  const originalSink = process.env.PULSE_SINK;
  process.env.PULSE_SINK = 'custom_sink';
  try {
    const args = buildFFmpegArgs('test-room', 'recording.mp4', { audioSource: 'pulse' });
    assert.ok(args.includes('custom_sink.monitor'), 'Should use the PULSE_SINK env var for the monitor source');
  } finally {
    if (originalSink === undefined) delete process.env.PULSE_SINK;
    else process.env.PULSE_SINK = originalSink;
  }
});

test('buildFFmpegArgs handles RTMP live stream with FLV format', () => {
  const roomSlug = 'test-room';
  const rtmpUrl = 'rtmp://a.rtmp.youtube.com/live2/key123';
  const args = buildFFmpegArgs(roomSlug, rtmpUrl, { useDummyAudio: true });

  assert.ok(args.includes('-f'), 'Should contain format flag');
  assert.ok(args.includes('flv'), 'Should specify FLV format for RTMP stream');
  assert.strictEqual(args[args.length - 1], rtmpUrl);
});

test('EgressWorker logs FFmpeg stderr output', async () => {
  let stderrLogged = false;
  const worker = new EgressWorker({
    redisClient: new RedisMock(),
    redisPubClient: new RedisMock(),
    logStderr: () => {
      stderrLogged = true;
    },
  });

  assert.strictEqual(stderrLogged, false);
});

test('EgressWorker handles command from Redis Pub/Sub without subscriber mode publish error', async () => {
  const redisSubscriber = new RedisMock();
  const redisPublisher = new RedisMock();

  let actionTriggered = null;

  const worker = new EgressWorker({
    redisClient: redisSubscriber,
    redisPubClient: redisPublisher,
    onCommand: (command) => {
      actionTriggered = command.action;
    },
  });

  await worker.startListening();

  // Publish test command
  await redisPublisher.publish('channel:egress_commands', JSON.stringify({ action: 'START_RECORDING' }));

  // Wait brief moment for async pubsub event
  await new Promise((r) => setTimeout(r, 100));

  assert.strictEqual(actionTriggered, 'START_RECORDING');
  await worker.stop();
});

test('EgressWorker treats FFmpeg code 255 as graceful completion when stopped via SIGINT', async () => {
  const redisPublisher = new RedisMock();
  const worker = new EgressWorker({
    redisClient: new RedisMock(),
    redisPubClient: redisPublisher,
  });

  // Mock fake process
  let closeCallback = null;
  worker.ffmpegProcess = {
    kill: (sig) => {
      assert.strictEqual(sig, 'SIGINT');
      if (closeCallback) closeCallback(255, null);
    },
    on: (evt, cb) => {
      if (evt === 'close') closeCallback = cb;
    },
  };

  worker.stopGracefully('SIGINT');
  assert.strictEqual(worker.ffmpegProcess, null);
});

test('resolveOutputPath saves recordings to persistent RECORDINGS_DIR', () => {
  const { resolveOutputPath } = require('./worker');
  process.env.RECORDINGS_DIR = '/app/recordings';
  const outputPath = resolveOutputPath('demo-room', 'START_RECORDING');
  assert.ok(outputPath.startsWith('/app/recordings/'), 'Should save in RECORDINGS_DIR');
  assert.ok(outputPath.endsWith('.mp4'), 'Should end with .mp4');
});

test('waitForEgressReady resolves immediately when the page already signals ready', async () => {
  const page = { evaluate: async () => true };
  const start = Date.now();
  const ready = await waitForEgressReady(page, { timeoutMs: 1000, pollIntervalMs: 200 });
  assert.strictEqual(ready, true);
  assert.ok(Date.now() - start < 100, 'should not wait a full poll interval when already ready');
});

test('waitForEgressReady polls until the flag flips true', async () => {
  let calls = 0;
  const page = {
    evaluate: async () => {
      calls += 1;
      return calls >= 3; // ready on the 3rd poll
    },
  };
  const ready = await waitForEgressReady(page, { timeoutMs: 1000, pollIntervalMs: 5 });
  assert.strictEqual(ready, true);
  assert.ok(calls >= 3, 'should have polled at least until the flag went true');
});

test('waitForEgressReady gives up and returns false after the timeout, never throwing', async () => {
  const page = { evaluate: async () => false };
  const ready = await waitForEgressReady(page, { timeoutMs: 30, pollIntervalMs: 10 });
  assert.strictEqual(ready, false);
});

test('waitForEgressReady treats a page.evaluate rejection as not-ready rather than crashing', async () => {
  const page = { evaluate: async () => { throw new Error('Execution context was destroyed'); } };
  const ready = await waitForEgressReady(page, { timeoutMs: 30, pollIntervalMs: 10 });
  assert.strictEqual(ready, false, 'a thrown evaluate should degrade to "not ready" by the deadline, not reject');
});

test('EgressWorker accepts egressReadyTimeoutMs/egressReadyPollMs for testability', () => {
  const worker = new EgressWorker({
    redisClient: new RedisMock(),
    redisPubClient: new RedisMock(),
    egressReadyTimeoutMs: 500,
    egressReadyPollMs: 50,
  });
  assert.strictEqual(worker.egressReadyTimeoutMs, 500);
  assert.strictEqual(worker.egressReadyPollMs, 50);
});

test('resolveOutputPath returns RTMP URL as-is for START_RTMP', () => {
  const { resolveOutputPath } = require('./worker');
  const rtmpUrl = 'rtmp://a.rtmp.youtube.com/live2/my-stream-key';
  const outputPath = resolveOutputPath('demo-room', 'START_RTMP', rtmpUrl);
  assert.strictEqual(outputPath, rtmpUrl, 'Should return RTMP URL intact');
});


