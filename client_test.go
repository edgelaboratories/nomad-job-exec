package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FIXME: these tests actually only test the mockClient...
func Test_allocationExec(t *testing.T) {
	t.Parallel()

	out, err := allocationExec(
		context.Background(),
		&mockClient{},
		&allocInfo{
			alloc: allocs["alloc-1"],
			task:  "task-1",
		},
		[]string{"ls"},
	)
	require.NoError(t, err)

	assert.Equal(t, "alloc-1", out.allocID)
	assert.Equal(t, 0, out.exitCode)
	assert.Equal(t, "stdout output", out.stdout)
	assert.Equal(t, "stderr output", out.stderr)
}

func Test_allocationExec_Failure(t *testing.T) {
	t.Parallel()

	out, err := allocationExec(
		context.Background(),
		&mockClient{
			failExec: true,
		},
		&allocInfo{
			alloc: allocs["alloc-1"],
			task:  "task-1",
		},
		[]string{"idontexist"},
	)
	require.NoError(t, err)

	assert.Equal(t, "alloc-1", out.allocID)
	assert.Equal(t, 1, out.exitCode)
	assert.Equal(t, "stdout output", out.stdout)
	assert.Equal(t, "stderr output", out.stderr)
}
