package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_allocationExec(t *testing.T) {
	t.Parallel()

	out, err := allocationExec(
		context.Background(),
		&mockClient{},
		&allocInfo{
			alloc: allocs["alloc-1"],
			task:  "task-1",
		},
		"foo",
		[]string{"ls"},
	)
	require.NoError(t, err)

	assert.Equal(t, "alloc-1", out.allocID)
	assert.Equal(t, 0, out.exitCode)
	assert.Equal(t, "stdout output", out.stdout)
	assert.Equal(t, "stderr output", out.stderr)
}
