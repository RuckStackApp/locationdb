// Package locationdb defines the durable multi-database layer that will sit on
// top of locationindex.
//
// Scope for this package right now:
//
//   - represent independently configured stores/databases
//   - define the JSON query request shape used by the API
//   - define the future query-language entry point shape
//   - provide repository scaffolding, tests, and release automation
//
// Intended query flow:
//
// JSON requests and future query-language requests will both normalize into a
// common query model that maps to:
//
//  1. spatial cells
//  2. candidate records
//  3. exact distance or validity filters
//
// This package is intentionally light at this stage. Storage engines, catalog
// management, mutation workflows, and API serving will be added incrementally.
package locationdb
