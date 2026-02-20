import http from "k6/http";
import { check, fail } from "k6";

const DEFAULT_SMD_URL = "http://127.0.0.1:27779";
const DEFAULT_CLOUD_INIT_URL = "http://127.0.0.1:27777";
const DEFAULT_INTERNAL_TOKEN =
  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

const baselines = JSON.parse(
  open(envOrDefault("CHAMICORE_LOAD_BASELINES_PATH", "./baselines.json")),
);

const smdBaseURL = envOrDefault("CHAMICORE_TEST_SMD_URL", DEFAULT_SMD_URL);
const cloudInitBaseURL = envOrDefault("CHAMICORE_TEST_CLOUDINIT_URL", DEFAULT_CLOUD_INIT_URL);
const internalToken = envOrDefault("CHAMICORE_INTERNAL_TOKEN", DEFAULT_INTERNAL_TOKEN);

const quickMode = envBool("QUICK");
const seedCount = envInt("CLOUDINIT_SEED_COUNT", quickMode ? 1000 : 10000);
const seedBatchSize = envInt("CLOUDINIT_SEED_BATCH_SIZE", 100);

const quickVUs = envInt("QUICK_VUS", 1000);
const quickDuration = envOrDefault("QUICK_DURATION", "2m");

const fullRampDuration = envOrDefault("CLOUDINIT_FULL_RAMP_DURATION", "2m");
const fullSustainDuration = envOrDefault("CLOUDINIT_FULL_SUSTAIN_DURATION", "10m");
const fullSpikeDuration = envOrDefault("CLOUDINIT_FULL_SPIKE_DURATION", "2m");
const fullCooldownDuration = envOrDefault("CLOUDINIT_FULL_COOLDOWN_DURATION", "1m");
const fullRampTargetVUs = envInt("CLOUDINIT_FULL_RAMP_TARGET_VUS", 10000);
const fullSpikeTargetVUs = envInt("CLOUDINIT_FULL_SPIKE_TARGET_VUS", 20000);

const cloudInitLatencyTarget = envInt(
  "CLOUDINIT_P99_TARGET_MS",
  baselines.cloud_init_storm.http_req_duration_p99_ms,
);
const cloudInitErrorTarget = envFloat(
  "CLOUDINIT_ERROR_RATE_MAX",
  baselines.cloud_init_storm.http_req_failed_rate_max,
);

export const options = quickMode
  ? {
      scenarios: {
        cloud_init_storm: {
          executor: "constant-vus",
          vus: quickVUs,
          duration: quickDuration,
        },
      },
      thresholds: {
        "http_req_duration{scenario:cloud_init_storm}": [`p(99)<${cloudInitLatencyTarget}`],
        "http_req_failed{scenario:cloud_init_storm}": [`rate<${cloudInitErrorTarget}`],
      },
    }
  : {
      scenarios: {
        cloud_init_storm: {
          executor: "ramping-vus",
          startVUs: 0,
          stages: [
            { duration: fullRampDuration, target: fullRampTargetVUs },
            { duration: fullSustainDuration, target: fullRampTargetVUs },
            { duration: fullSpikeDuration, target: fullSpikeTargetVUs },
            { duration: fullCooldownDuration, target: 0 },
          ],
        },
      },
      thresholds: {
        "http_req_duration{scenario:cloud_init_storm}": [`p(99)<${cloudInitLatencyTarget}`],
        "http_req_failed{scenario:cloud_init_storm}": [`rate<${cloudInitErrorTarget}`],
      },
    };

export function setup() {
  const runID = Date.now().toString(36);
  const headers = jsonHeaders(internalToken);

  for (let start = 0; start < seedCount; start += seedBatchSize) {
    const end = Math.min(start + seedBatchSize, seedCount);

    const componentRequests = [];
    for (let i = start; i < end; i += 1) {
      componentRequests.push({
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

    const componentResponses = http.batch(componentRequests);
    for (let i = 0; i < componentResponses.length; i += 1) {
      const response = componentResponses[i];
      if (response.status !== 201 && response.status !== 409) {
        fail(
          `seed component failed at index=${start + i} status=${response.status} body=${response.body}`,
        );
      }
    }

    const payloadRequests = [];
    for (let i = start; i < end; i += 1) {
      const component = componentID(runID, i);
      payloadRequests.push({
        method: "POST",
        url: `${cloudInitBaseURL}/cloud-init/payloads`,
        body: JSON.stringify({
          component_id: component,
          role: "Compute",
          user_data: `#cloud-config\nhostname: ${component}\n`,
          meta_data: { "instance-id": component },
          vendor_data: "vendor: load\n",
        }),
        params: { headers },
      });
    }

    const payloadResponses = http.batch(payloadRequests);
    for (let i = 0; i < payloadResponses.length; i += 1) {
      const response = payloadResponses[i];
      if (response.status !== 201 && response.status !== 409) {
        fail(
          `seed payload failed at index=${start + i} status=${response.status} body=${response.body}`,
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
    `${cloudInitBaseURL}/cloud-init/${encodeURIComponent(component)}/user-data`,
    { tags: { endpoint: "user-data" } },
  );

  check(response, {
    "user-data status is 200": (r) => r.status === 200,
    "user-data includes cloud-config": (r) => r.body && r.body.includes("#cloud-config"),
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
  return `node-cloud-load-${runID}-${index}`;
}
