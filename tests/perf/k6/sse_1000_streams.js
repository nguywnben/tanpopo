import http from 'k6/http';
import { check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.TANPOPO_BASE_URL || 'http://127.0.0.1:8080';
const VKEY = __ENV.TANPOPO_VK || '';
const MODEL = __ENV.TANPOPO_DECL_MODEL || 'bench-decl';
const SSE_TARGET_VUS = Number(__ENV.SSE_VUS || 1000);
const RPS_RATE = Number(__ENV.RPS_RATE || 500);
const PHASE_DURATION = __ENV.PHASE_DURATION || '15s';

export const sseTimeToFirstByteMs = new Trend('tanpopo_sse_ttfb_ms');
export const shortReqLatencyMs = new Trend('tanpopo_short_req_ms');
export const sseDoneRate = new Rate('tanpopo_sse_done_ok');
export const internal5xxCount = new Counter('tanpopo_internal_5xx_count');

export const options = {
  scenarios: {
    sse_1000_concurrent: {
      executor: 'ramping-vus',
      startVUs: 100,
      stages: [
        { duration: '5s', target: SSE_TARGET_VUS },
        { duration: PHASE_DURATION, target: SSE_TARGET_VUS },
        { duration: '3s', target: 0 },
      ],
      exec: 'runSSEStream',
      gracefulStop: '10s',
    },
    short_500_rps: {
      executor: 'constant-arrival-rate',
      rate: RPS_RATE,
      timeUnit: '1s',
      duration: PHASE_DURATION,
      startTime: '5s',
      preAllocatedVUs: 150,
      maxVUs: 600,
      exec: 'runShortRequest',
      gracefulStop: '5s',
    },
  },
  thresholds: {
    tanpopo_internal_5xx_count: ['count==0'],
    tanpopo_sse_done_ok: ['rate>0.9995'],
  },
};

function authHeaders(extra) {
  const hdrs = {
    'Content-Type': 'application/json',
    Authorization: `Bearer ${VKEY}`,
  };
  if (extra) {
    Object.assign(hdrs, extra);
  }
  return hdrs;
}

export function runSSEStream() {
  const payload = JSON.stringify({
    model: MODEL,
    stream: true,
    messages: [{ role: 'user', content: 'slo-03-06-sse-benchmark' }],
  });
  const res = http.post(`${BASE_URL}/v1/chat/completions`, payload, {
    headers: authHeaders(),
    timeout: '30s',
  });
  if (res.status === 0 || res.status >= 500) {
    internal5xxCount.add(1);
  }
  const doneOK = check(res, {
    'sse status 200': (r) => r.status === 200,
    'sse finished [DONE]': (r) => typeof r.body === 'string' && r.body.includes('[DONE]'),
  });
  sseDoneRate.add(doneOK);
  if (res.timings && typeof res.timings.waiting === 'number') {
    sseTimeToFirstByteMs.add(res.timings.waiting);
  }
}

export function runShortRequest() {
  const payload = JSON.stringify({
    model: MODEL,
    stream: false,
    messages: [{ role: 'user', content: 'slo-06-500rps' }],
  });
  const res = http.post(`${BASE_URL}/v1/chat/completions`, payload, {
    headers: authHeaders(),
    timeout: '10s',
  });
  if (res.status === 0 || res.status >= 500) {
    internal5xxCount.add(1);
  }
  check(res, {
    'short status 200': (r) => r.status === 200,
  });
  if (res.timings && typeof res.timings.duration === 'number') {
    shortReqLatencyMs.add(res.timings.duration);
  }
}
