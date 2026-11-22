import http from 'k6/http';
import { check } from 'k6';
import { Counter, Rate } from 'k6/metrics';

const BASE_URL = __ENV.TANPOPO_BASE_URL || 'http://127.0.0.1:8080';
const VKEY = __ENV.TANPOPO_VK || '';
const MODEL = __ENV.TANPOPO_DECL_MODEL || 'bench-decl';
const ACTIVE_VUS = Number(__ENV.SSE_VUS || 100);
const DURATION = __ENV.DURATION || '12s';

export const streamCompletedOK = new Rate('tanpopo_rolling_sse_ok');
export const droppedStreams = new Counter('tanpopo_rolling_dropped_streams');

export const options = {
  scenarios: {
    ops_ac_06_rolling_streams: {
      executor: 'constant-vus',
      vus: ACTIVE_VUS,
      duration: DURATION,
      gracefulStop: '10s',
    },
  },
  thresholds: {
    tanpopo_rolling_dropped_streams: ['count==0'],
    tanpopo_rolling_sse_ok: ['rate==1.0'],
  },
};

export default function () {
  const res = http.post(
    `${BASE_URL}/v1/chat/completions`,
    JSON.stringify({
      model: MODEL,
      stream: true,
      messages: [{ role: 'user', content: 'ops-ac-06-rolling-restart' }],
    }),
    {
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${VKEY}`,
      },
      timeout: '20s',
    },
  );
  const ok = check(res, {
    'rolling SSE status 200': (r) => r.status === 200,
    'rolling SSE [DONE]': (r) => typeof r.body === 'string' && r.body.includes('[DONE]'),
  });
  streamCompletedOK.add(ok);
  if (!ok) {
    droppedStreams.add(1);
  }
}
