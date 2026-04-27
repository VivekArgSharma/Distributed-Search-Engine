import http from 'k6/http';
import { check, sleep } from 'k6';

const baseUrl = __ENV.BASE_URL || 'http://host.docker.internal:8090';
const query = __ENV.QUERY || 'programming';
const mode = __ENV.MODE || 'bm25';
const limit = __ENV.LIMIT || '5';

export const options = {
  vus: Number(__ENV.VUS || 20),
  duration: __ENV.DURATION || '30s',
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(50)<100', 'p(95)<100'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const url = `${baseUrl}/search?query=${encodeURIComponent(query)}&mode=${encodeURIComponent(mode)}&limit=${limit}`;
  const response = http.get(url, {
    tags: { endpoint: 'search', mode },
  });

  check(response, {
    'status is 200': (r) => r.status === 200,
    'has results array': (r) => {
      try {
        const body = JSON.parse(r.body);
        return Array.isArray(body.results);
      } catch (_) {
        return false;
      }
    },
  });

  sleep(Number(__ENV.SLEEP || 0));
}
