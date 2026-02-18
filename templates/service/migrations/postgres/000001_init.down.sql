-- TEMPLATE: Rollback migration for __SERVICE_FULL__
-- Copy this file and replace all __PLACEHOLDER__ markers with your service values.
--
-- Migration: 000001_init (DOWN)
-- Description: Drops the __RESOURCE_TABLE__ table, the trigger function, and
--              the __SCHEMA__ schema.
--
-- WARNING: This is destructive. All data in the __RESOURCE_TABLE__ table will be lost.

-- Drop the trigger first (depends on the table).
DROP TRIGGER IF EXISTS trg___RESOURCE_TABLE___updated_at ON __SCHEMA__.__RESOURCE_TABLE__;

-- Drop the trigger function.
DROP FUNCTION IF EXISTS __SCHEMA__.set_updated_at();

-- Drop the table.
DROP TABLE IF EXISTS __SCHEMA__.__RESOURCE_TABLE__;

-- Drop the schema (only if empty — CASCADE would be dangerous here).
DROP SCHEMA IF EXISTS __SCHEMA__;
