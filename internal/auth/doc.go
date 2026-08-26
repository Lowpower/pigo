// Package auth stores provider credentials in auth.json and resolves them for
// requests and CLI commands.
//
// Ported from pi:
//
//	packages/coding-agent/src/core/auth-storage.ts
//	packages/ai/src/auth/types.ts
//	packages/ai/src/auth/resolve.ts
//	packages/coding-agent/src/migrations.ts (migrateAuthToAuthJson)
//	packages/coding-agent/src/core/resolve-config-value.ts
//	packages/coding-agent/src/cli/credential-print.ts
//	packages/coding-agent/src/cli/auth-check.ts
package auth
