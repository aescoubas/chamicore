import http from "k6/http";
import { check, fail } from "k6";

const DEFAULT_SMD_URL = "http://127.0.0.1:27779";
const DEFAULT_INTERNAL_TOKEN =
  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

const baselines = JSON.parse(
  open(envOrDefault("CHAMICORE_LOAD_BASELINES_PATH", "./baselines.json")),
);

const smdBaseURL = envOrDefault("CHAMICORE_TEST_SMD_URL", DEFAULT_SMD_URL);
const internalToken = envOrDefault("CHAMICORE_INTERNAL_TOKEN", DEFAULT_INTERNAL_TOKEN);

const quickMode = envBool("QUICK");
const seedCount = envInt(
  "INVENTORY_SEED_COUNT",
  quickMode ? 1000 : 50000,
);
const seedBatchSize = envInt("INVENTORY_SEED_BATCH_SIZE", 100);

const quickVUs = envInt("QUICK_VUS", 1000);
const quickDuration = envOrDefault("QUICK_DURATION", "2m");

const fullVUs = envInt("INVENTORY_FULL_VUS", 2000);
const fullDuration = envOrDefault("INVENTORY_FULL_DURATION", "10m");

const inventoryLatencyTarget = envInt(
  "INVENTORY_P99_TARGET_MS",
  baselines.inventory_scale.http_req_duration_p99_ms,
);
const inventoryErrorTarget = envFloat(
  "INVENTORY_ERROR_RATE_MAX",
  baselines.inventory_scale.http_req_failed_rate_max,
);

export const options = quickMode
  ? {
      scenarios: {
        inventory_scale: {
          executor: "constant-vus",
          vus: quickVUs,
          duration: quickDuration,
        },
      },
      thresholds: {
        "http_req_duration{scenario:inventory_scale}": [`p(99)<${inventoryLatencyTarget}`],
        "http_req_failed{scenario:inventory_scale}": [`rate<${inventoryErrorTarget}`],
      },
    }
  : {
      scenarios: {
        inventory_scale: {
          executor: "constant-vus",
          vus: fullVUs,
          duration: fullDuration,
        },
      },
      thresholds: {
        "http_req_duration{scenario:inventory_scale}": [`p(99)<${inventoryLatencyTarget}`],
        "http_req_failed{scenario:inventory_scale}": [`rate<${inventoryErrorTarget}`],
      },
    };

export function setup() {
  const runID = Date.now().toString(36);
  const headers = jsonHeaders(internalToken);

  for (let start = 0; start < seedCount; start += seedBatchSize) {
    const end = Math.min(start + seedBatchSize, seedCount);
    const requests = [];

    for (let i = start; i < end; i += 1) {
      requests.push({
        method: "POST",
        url: `${smdBaseURL}/hsm/v2/State/Components`,
        body: JSON.stringify({
          id: componentID(runID, i),
          type: "Node",
          state: "Ready",
          role: "Compute",
          slot: 0,
          subSlot: 0,
        }),
        params: { headers },
      });
    }

    const responses = http.batch(requests);
    for (let i = 0; i < responses.length; i += 1) {
      const response = responses[i];
      if (response.status !== 201 && response.status !== 409) {
        fail(
          `seed inventory failed at index=${start + i} status=${response.status} body=${response.body}`,
        );
      }
    }
  }

  return {
    runID,
    seedCount,
  };
}

export default function (data) {
  const index = Math.floor(Math.random() * data.seedCount);
  const component = componentID(data.runID, index);

  const response = http.get(
    `${smdBaseURL}/hsm/v2/State/Components/${encodeURIComponent(component)}`,
    {
      headers: {
        Authorization: `Bearer ${internalToken}`,
      },
      tags: {
        endpoint: "component-get",
      },
    },
  );

  check(response, {
    "component status is 200": (r) => r.status === 200,
    "component response has id": (r) => r.body && r.body.includes(`"id":"${component}"`),
  });
}

function envOrDefault(name, fallback) {
  const value = __ENV[name];
  if (typeof value !== "string") {
    return fallback;
  }

  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : fallback;
}

function envInt(name, fallback) {
  const raw = envOrDefault(name, String(fallback));
  const parsed = Number.parseInt(raw, 10);
  return Number.isNaN(parsed) ? fallback : parsed;
}

function envFloat(name, fallback) {
  const raw = envOrDefault(name, String(fallback));
  const parsed = Number.parseFloat(raw);
  return Number.isNaN(parsed) ? fallback : parsed;
}

function envBool(name) {
  const value = envOrDefault(name, "false").toLowerCase();
  return value === "1" || value === "true" || value === "yes";
}

function jsonHeaders(token) {
  return {
    Authorization: `Bearer ${token}`,
    "Content-Type": "application/json",
  };
}

function componentID(runID, index) {
  return `node-inventory-load-${runID}-${index}`;
}
