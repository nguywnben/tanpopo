import http from 'k6/http';
import { check } from 'k6';
import { Trend } from 'k6/metrics';

const BASE_URL = __ENV.TANPOPO_BASE_URL || 'http://127.0.0.1:8080';
const VKEY = __ENV.TANPOPO_VK || '';
const DECL_MODEL = __ENV.TANPOPO_DECL_MODEL || 'bench-decl';
const WASM_MODEL = __ENV.TANPOPO_WASM_MODEL || 'bench-wasm';
const RPS_RATE = Number(__ENV.RPS_RATE || 500);
const PHASE_SEC = Number(__ENV.PHASE_SEC || 15);

export const tier1LatencyMs = new Trend('tanpopo_tier1_declar_ms');
export const tier2LatencyMs = new Trend('tanpopo_tier2_wasm_ms');

export const options = {
  scenarios: {
    tier1_warmup: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '3s',
      preAllocatedVUs: 32,
      maxVUs: 100,
      exec: 'runTier1Warmup',
    },
    tier1_declarative: {
      executor: 'constant-arrival-rate',
      rate: RPS_RATE,
      timeUnit: '1s',
      duration: `${PHASE_SEC}s`,
      startTime: '4s',
      preAllocatedVUs: 120,
      maxVUs: 400,
      exec: 'runTier1',
    },
    tier2_warmup: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '3s',
      startTime: `${PHASE_SEC + 5}s`,
      preAllocatedVUs: 32,
      maxVUs: 100,
      exec: 'runTier2Warmup',
    },
    tier2_wasm: {
      executor: 'constant-arrival-rate',
      rate: RPS_RATE,
      timeUnit: '1s',
      duration: `${PHASE_SEC}s`,
      startTime: `${PHASE_SEC + 9}s`,
      preAllocatedVUs: 120,
      maxVUs: 400,
      exec: 'runTier2',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.0005'],
  },
};

function postModel(model) {
  const payload = JSON.stringify({
    model,
    stream: false,
    messages: [{ role: 'user', content: 'slo-02-latency-probe' }],
  });
  return http.post(`${BASE_URL}/v1/chat/completions`, payload, {
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${VKEY}`,
    },
    timeout: '10s',
  });
}

export function runTier1Warmup() {
  postModel(DECL_MODEL);
}

export function runTier1() {
  const res = postModel(DECL_MODEL);
  check(res, { 'tier1 200 OK': (r) => r.status === 200 });
  if (res.timings) {
    tier1LatencyMs.add(res.timings.duration);
  }
}

export function runTier2Warmup() {
  postModel(WASM_MODEL);
}

export function runTier2() {
  const res = postModel(WASM_MODEL);
  check(res, { 'tier2 200 OK': (r) => r.status === 200 });
  if (res.timings) {
    tier2LatencyMs.add(res.timings.duration);
  }
}
