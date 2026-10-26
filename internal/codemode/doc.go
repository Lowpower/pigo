// Package codemode runs one JavaScript script at a time in a goja sandbox.
//
// Each script is a child process with a heap target and an OS memory cap, so a
// runaway allocation takes down the child and leaves this process running.
// At most a few children are alive at once. Callers past the limit wait in
// arrival order. The script API (tools, models, output limits) is not part of
// this package.
package codemode
