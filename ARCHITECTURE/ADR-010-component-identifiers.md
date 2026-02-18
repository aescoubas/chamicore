# ADR-010: Component Identifiers (Flat IDs + Location Metadata)

## Status

Accepted

## Date

2025-02-18

## Context

Upstream OpenCHAMI (inherited from Cray CSM) uses **xnames** as the primary identifier
for all hardware components. An xname is a hierarchical string encoding the physical
cabinet/chassis/slot location of a component (e.g., `x3000c0s17b0n0` = cabinet 3000,
chassis 0, slot 17, BMC 0, node 0).

xnames have several problems for a general-purpose HPC management platform:

1. **HPE/Cray-specific**: The format encodes Cray EX topology assumptions (8 chassis per
   liquid-cooled cabinet, Slingshot HSN switches, Cray-specific FPGAs). ~40+ component types
   with individual regex patterns require a dedicated parsing library.

2. **Meaningless for standard racks**: In non-Cray hardware, chassis is always `c0`, making
   that segment of the xname pure noise.

3. **Identifies a slot, not hardware**: If a blade moves from slot 17 to slot 19, every
   identifier in the system changes. The xname tracks *where* something is, not *what* it is.

4. **Synthetic generation for non-Cray hardware**: OpenCHAMI's `magellan` tool generates
   xnames from IP address octets for non-Cray systems, defeating the purpose of location encoding.

5. **Deep coupling**: xname is the `PRIMARY KEY` in every SMD database table and appears in
   every API path. Changing it later would be extremely disruptive.

Since Chamicore is a clean-room rewrite targeting general HPC clusters (not just Cray EX),
we need an identifier scheme that works for any rack-mount hardware.

## Decision

Chamicore will use **flat opaque string identifiers** as the primary key for components,
with **physical location as queryable metadata**.

### Component ID

- Primary identifier is an opaque string: `id` field, `VARCHAR(255) PRIMARY KEY`.
- IDs are assigned at registration time. The system provides a default generator but
  operators can supply their own IDs.
- Default ID format: `<type>-<short-hash>` (e.g., `node-a1b2c3`, `bmc-d4e5f6`).
- IDs are **immutable** once assigned. Hardware moves do not change the ID.
- IDs must be non-empty, ASCII, no whitespace, safe for use in URLs.
- Validation regex: `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,253}[a-zA-Z0-9]$`

### Location Metadata

Physical location is stored as structured metadata on each component, not encoded in the ID:

```json
{
  "kind": "Component",
  "apiVersion": "hsm/v2",
  "metadata": {
    "id": "node-a1b2c3",
    "etag": "...",
    "createdAt": "...",
    "updatedAt": "..."
  },
  "spec": {
    "type": "Node",
    "state": "Ready",
    "role": "Compute",
    "nid": 1001,
    "location": {
      "rack": "R12",
      "unit": 17,
      "slot": 0,
      "subSlot": 0
    },
    "networkInterfaces": [...],
    "bmcId": "bmc-d4e5f6"
  }
}
```

### Location Schema

```sql
CREATE TABLE components (
    id          VARCHAR(255) PRIMARY KEY,
    type        VARCHAR(63)  NOT NULL,
    state       VARCHAR(32)  NOT NULL,
    role        VARCHAR(32)  NOT NULL DEFAULT '',
    nid         BIGINT,
    rack        TEXT,
    unit        INT,
    slot        INT DEFAULT 0,
    sub_slot    INT DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_components_location ON components (rack, unit, slot);
CREATE INDEX idx_components_type ON components (type);
CREATE INDEX idx_components_state ON components (state);
CREATE INDEX idx_components_role ON components (role);
```

### Location Fields

| Field | Type | Description | Example |
|-------|------|-------------|---------|
| `rack` | string | Rack/cabinet identifier | `"R12"`, `"cab-3000"` |
| `unit` | int | Rack unit position (U-number) | `17` |
| `slot` | int | Slot within a multi-blade unit (default 0 for 1U servers) | `0` |
| `subSlot` | int | Sub-component index (e.g., node within a multi-node blade) | `0` |

Location fields are **optional** - components can exist without location data (e.g.,
discovered via Redfish but not yet physically mapped).

### Component Hierarchy

Instead of encoding hierarchy in the ID string, relationships are expressed via
foreign key references:

```sql
-- A node belongs to a BMC
ALTER TABLE components ADD COLUMN parent_id VARCHAR(255)
    REFERENCES components(id);
```

- `parent_id` links a component to its parent (e.g., node -> BMC -> slot).
- Hierarchy queries use recursive CTEs or simple joins.
- This is more flexible than string-prefix matching: a node can be re-parented
  when hardware moves without changing its ID.

### Querying by Location

```
GET /hsm/v2/State/Components?rack=R12
GET /hsm/v2/State/Components?rack=R12&unit=17
GET /hsm/v2/State/Components?type=Node&role=Compute
```

### Component Types

We define a reduced set of component types relevant to general HPC clusters:

| Type | Description |
|------|-------------|
| `Cabinet` | Rack or cabinet |
| `BMC` | Baseboard Management Controller (Redfish endpoint) |
| `Node` | Compute or service node |
| `Processor` | CPU socket |
| `Memory` | DIMM |
| `Accelerator` | GPU or other accelerator |
| `NIC` | Network interface |
| `Drive` | Storage device |
| `PDU` | Power distribution unit |
| `Switch` | Network switch |

This is ~10 types vs ~40+ in the Cray xname system. New types can be added as needed
without modifying a regex recognition table.

### xname Compatibility Layer

For sites migrating from OpenCHAMI or using tools that expect xnames:

- Operators can use xname-format strings as component IDs (they satisfy the ID regex).
- An optional `xname` field in location metadata stores the xname for reference.
- The CLI can accept xnames and resolve them to component IDs.
- This is a **convenience**, not a requirement. Chamicore does not parse or validate
  xname structure internally.

## Consequences

### Positive

- **Hardware-stable identifiers**: A component's ID survives physical moves. Updating
  location metadata is a simple PATCH, not a cascading rename across every table.
- **No Cray assumptions**: Works equally well for standard rack-mount servers, Cray EX
  blades, cloud instances, or any future hardware topology.
- **Simpler codebase**: No xname parsing library, no 40-regex recognition table, no
  `GetHMSCompParent()` string manipulation. Type is an explicit field, not inferred.
- **Flexible hierarchy**: Parent-child relationships via foreign keys are more expressive
  than string-prefix hierarchy. Easy to query ancestors/descendants with SQL.
- **Operator-friendly**: Operators can assign meaningful IDs (`gpu-node-01`,
  `login-node-east`) or let the system auto-generate.
- **URL-safe**: IDs are safe for use in API paths without encoding.

### Negative

- **No hierarchy from ID alone**: Cannot derive a parent by trimming an ID string.
  Requires a database lookup via `parent_id`.
  - Mitigated: This is a single indexed join. Hierarchy queries are infrequent
    compared to direct component lookups.
- **Migration effort from OpenCHAMI**: Sites with existing xname-based tooling need
  to update their workflows.
  - Mitigated: xname strings can be used as IDs verbatim. The compatibility layer
    accepts xnames without requiring Chamicore to understand their structure.
- **No type inference from ID**: Cannot tell if `node-a1b2c3` is a Node without
  querying the database.
  - Mitigated: The `type` field is always present in API responses. Type-specific
    API endpoints can also be used (`GET /hsm/v2/State/Components?type=Node`).

### Neutral

- NID (Node ID) assignment remains a separate mapping, as in upstream OpenCHAMI.
- Redfish discovery creates components with auto-generated IDs; operators can rename
  them via PATCH.
- The location schema can be extended with additional fields (building, room, row, cage)
  without changing the identifier scheme.
