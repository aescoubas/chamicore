import http from "k6/http";
import { check, fail } from "k6";

const DEFAULT_SMD_URL = "http://127.0.0.1:27779";
const DEFAULT_BSS_URL = "http://127.0.0.1:27778";
const DEFAULT_INTERNAL_TOKEN =
  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

const baselines = JSON.parse(
  open(envOrDefault("CHAMICORE_LOAD_BASELINES_PATH", "./baselines.json")),
);

const smdBaseURL = envOrDefault("CHAMICORE_TEST_SMD_URL", DEFAULT_SMD_URL);
const bssBaseURL = envOrDefault("CHAMICORE_TEST_BSS_URL", DEFAULT_BSS_URL);
const internalToken = envOrDefault("CHAMICORE_INTERNAL_TOKEN", DEFAULT_INTERNAL_TOKEN);

const quickMode = envBool("QUICK");
const seedCount = envInt("BOOT_SEED_COUNT", quickMode ? 1000 : 10000);
const seedBatchSize = envInt("BOOT_SEED_BATCH_SIZE", 100);

const quickVUs = envInt("QUICK_VUS", 1000);
const quickDuration = envOrDefault("QUICK_DURATION", "2m");

const fullRampDuration = envOrDefault("BOOT_FULL_RAMP_DURATION", "2m");
const fullSustainDuration = envOrDefault("BOOT_FULL_SUSTAIN_DURATION", "10m");
const fullSpikeDuration = envOrDefault("BOOT_FULL_SPIKE_DURATION", "2m");
const fullCooldownDuration = envOrDefault("BOOT_FULL_COOLDOWN_DURATION", "1m");
const fullRampTargetVUs = envInt("BOOT_FULL_RAMP_TARGET_VUS", 10000);
const fullSpikeTargetVUs = envInt("BOOT_FULL_SPIKE_TARGET_VUS", 20000);

const bootLatencyTarget = envInt(
  "BOOT_P99_TARGET_MS",
  baselines.boot_storm.http_req_duration_p99_ms,
);
const bootErrorTarget = envFloat(
  "BOOT_ERROR_RATE_MAX",
  baselines.boot_storm.http_req_failed_rate_max,
);

export const options = quickMode
  ? {
      scenarios: {
        boot_storm: {
          executor: "constant-vus",
          vus: quickVUs,
          duration: quickDuration,
        },
      },
      thresholds: {
        "http_req_duration{scenario:boot_storm}": [`p(99)<${bootLatencyTarget}`],
        "http_req_failed{scenario:boot_storm}": [`rate<${bootErrorTarget}`],
      },
    }
  : {
      scenarios: {
        boot_storm: {
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
        "http_req_duration{scenario:boot_storm}": [`p(99)<${bootLatencyTarget}`],
        "http_req_failed{scenario:boot_storm}": [`rate<${bootErrorTarget}`],
      },
    };

export function setup() {
  const runID = Date.now().toString(36);
  const runSeed = hashString(runID);
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

    const interfaceRequests = [];
    for (let i = start; i < end; i += 1) {
      interfaceRequests.push({
        method: "POST",
        url: `${smdBaseURL}/hsm/v2/Inventory/EthernetInterfaces`,
        body: JSON.stringify({
          componentId: componentID(runID, i),
          macAddr: macAddress(runSeed, i),
        }),
        params: { headers },
      });
    }

    const interfaceResponses = http.batch(interfaceRequests);
    for (let i = 0; i < interfaceResponses.length; i += 1) {
      const response = interfaceResponses[i];
      if (response.status !== 201 && response.status !== 409) {
        fail(
          `seed interface failed at index=${start + i} status=${response.status} body=${response.body}`,
        );
      }
    }

    const bootParamRequests = [];
    for (let i = start; i < end; i += 1) {
      const component = componentID(runID, i);
      bootParamRequests.push({
        method: "POST",
        url: `${bssBaseURL}/boot/v1/bootparams`,
        body: JSON.stringify({
          component_id: component,
          mac: macAddress(runSeed, i),
          role: "Compute",
          kernel_uri: `https://boot.load.local/${component}/vmlinuz`,
          initrd_uri: `https://boot.load.local/${component}/initrd.img`,
          cmdline: "console=ttyS0",
        }),
        params: { headers },
      });
    }

    const bootParamResponses = http.batch(bootParamRequests);
    for (let i = 0; i < bootParamResponses.length; i += 1) {
      const response = bootParamResponses[i];
      if (response.status !== 201 && response.status !== 409) {
        fail(
          `seed bootparam failed at index=${start + i} status=${response.status} body=${response.body}`,
        );
      }
    }
  }

  return {
    runID,
    runSeed,
    seedCount,
  };
}

export default function (data) {
  const index = Math.floor(Math.random() * data.seedCount);
  const mac = macAddress(data.runSeed, index);

  const response = http.get(
    `${bssBaseURL}/boot/v1/bootscript?mac=${encodeURIComponent(mac)}`,
    { tags: { endpoint: "bootscript" } },
  );

  check(response, {
    "bootscript status is 200": (r) => r.status === 200,
    "bootscript contains shebang": (r) => r.body && r.body.includes("#!ipxe"),
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
  return `node-load-${runID}-${index}`;
}

function macAddress(runSeed, index) {
  const b1 = (runSeed >> 8) & 0xff;
  const b2 = runSeed & 0xff;
  const b3 = (index >> 16) & 0xff;
  const b4 = (index >> 8) & 0xff;
  const b5 = index & 0xff;

  return `02:${hexByte(b1)}:${hexByte(b2)}:${hexByte(b3)}:${hexByte(b4)}:${hexByte(b5)}`;
}

function hexByte(value) {
  return value.toString(16).padStart(2, "0");
}

function hashString(value) {
  let hash = 0;
  for (let i = 0; i < value.length; i += 1) {
    hash = (hash * 31 + value.charCodeAt(i)) & 0xffff;
  }
  return hash;
}
