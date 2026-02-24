DROP INDEX IF EXISTS power.idx_node_bmc_links_bmc_id;
DROP INDEX IF EXISTS power.idx_transition_tasks_state;
DROP INDEX IF EXISTS power.idx_transition_tasks_node_id;
DROP INDEX IF EXISTS power.idx_transition_tasks_transition_id;
DROP INDEX IF EXISTS power.idx_transitions_operation;
DROP INDEX IF EXISTS power.idx_transitions_state;

DROP TABLE IF EXISTS power.node_bmc_links;
DROP TABLE IF EXISTS power.bmc_endpoints;
DROP TABLE IF EXISTS power.transition_tasks;
DROP TABLE IF EXISTS power.transitions;

DROP SCHEMA IF EXISTS power;
