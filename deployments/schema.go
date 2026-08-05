package deployments

import _ "embed"

// SchemaSQL is the single complete MySQL DDL (CREATE TABLE only).
//
//go:embed schema.sql
var SchemaSQL string
