// Package schemaversion is the engine API/schema generation GUI packages declare
// support for. It is intentionally tiny so daemons, catalogs, and tests share
// one number.
package schemaversion

// Engine is the schema version implemented by this daemon build.
// GUI packages declare an inclusive min/max range against this value.
const Engine = 1
