import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.TANPOPO_BASE_URL || 'http://127.0.0.1:8080';
const VKEY = __ENV.TANPOPO_VK || '';
const MODEL = __ENV.TANPOPO_DECL_MODEL || 'bench-decl';
const HOLD_VUS = Number(__ENV.HOLD_VUS || 50);

export const shed503DurationMs = new Trend('tanpopo_shed_503_ms');
export const shed503Count = new Counter('tanpopo_shed_503_count');
export const inflightSseOK = new Rate('tanpopo_inflight_sse_ok');
export const recoveredOK = new Rate('tanpopo_recovered_ok');

export const options = {
  scenarios: {
    hold_streams_at_ceiling: {
      executor: 'ramping-vus',
      startVUs: HOLD_VUS,
      stages: [
        { duration: '6s', target: HOLD_VUS },
        { duration: '1s', target: 0 },
      ],
      exec: 'holdSSE',
      gracefulStop: '3s',
    },
    excess_3x_overload: {
      executor: 'constant-arrival-rate',
      rate: 300,
      timeUnit: '1s',
      duration: '4s',
      startTime: '1s',
      preAllocatedVUs: 60,
      maxVUs: 200,
      exec: 'burstOverload',
    },
    healthz_responsive_during_overload: {
      executor: 'constant-arrival-rate',
      rate: 20,
      timeUnit: '1s',
      duration: '5s',
      startTime: '1s',
      preAllocatedVUs: 5,
      maxVUs: 20,
      exec: 'checkHealthz',
    },
    post_overload_recovery: {
      executor: 'constant-arrival-rate',
      rate: 50,
      timeUnit: '1s',
      duration: '3s',
      startTime: '9s',
      preAllocatedVUs: 20,
      maxVUs: 50,
      exec: 'verifyRecovery',
    },
  },
  thresholds: {
    tanpopo_inflight_sse_ok: ['rate==1.0'],
    tanpopo_recovered_ok: ['rate==1.0'],
    tanpopo_shed_503_count: ['count>0'],
  },
};

export function holdSSE() {
  const res = http.post(
    `${BASE_URL}/v1/chat/completions`,
    JSON.stringify({
      model: MODEL,
      stream: true,
      messages: [{ role: 'user', content: 'neg-1-inflight-stream' }],
    }),
    {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${VKEY}`,
      },
      timeout: '15s',
    },
  );
  if (res.status === 503 && typeof res.body === 'string' && res.body.includes('gateway_overloaded')) {
    shed503Count.add(1);
    return;
  }
  const ok = check(res, {
    'held stream 200 [DONE]': (r) => r.status === 200 && typeof r.body === 'string' && r.body.includes('[DONE]'),
  });
  inflightSseOK.add(ok);
}

export function burstOverload() {
  const res = http.post(
    `${BASE_URL}/v1/chat/completions`,
    JSON.stringify({
      model: MODEL,
      stream: false,
      messages: [{ role: 'user', content: 'neg-1-3x-burst' }],
    }),
    {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${VKEY}`,
      },
      timeout: '5s',
    },
  );
  check(res, {
    'burst status 503 or 200': (r) => r.status === 503 || r.status === 200,
  });
  if (res.status === 503 && typeof res.body === 'string' && res.body.includes('gateway_overloaded')) {
    shed503Count.add(1);
    if (res.timings) {
      shed503DurationMs.add(res.timings.duration);
    }
    check(res, {
      '503 Retry-After present': (r) => Boolean(r.headers['Retry-After'] || r.headers['retry-after']),
    });
  }
}

export function checkHealthz() {
  const res = http.get(`${BASE_URL}/healthz`, { timeout: '2s' });
  check(res, { 'healthz 200 under overload': (r) => r.status === 200 });
}

export function verifyRecovery() {
  sleep(0.01);
  const res = http.post(
    `${BASE_URL}/v1/chat/completions`,
    JSON.stringify({
      model: MODEL,
      stream: false,
      messages: [{ role: 'user', content: 'neg-1-recovered' }],
    }),
    {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${VKEY}`,
      },
      timeout: '5s',
    },
  );
  const ok = check(res, { 'recovered 200 OK': (r) => r.status === 200 });
  recoveredOK.add(ok);
}
