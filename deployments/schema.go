package deployments

import _ "embed"

// SchemaSQL is the single complete MySQL DDL (CREATE TABLE only).
// Source: deployments/db/schema.sql — no ALTER migrations.
//
//go:embed db/schema.sql
var SchemaSQL string
