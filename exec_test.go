package main

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/hashicorp/nomad/api"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nomadClientMock struct{}

func (c *nomadClientMock) NodeInfo(nodeID string) (*nodeInfo, error) {
	if nodeID != "fake-node-id" {
		return nil, errors.New("failed to find the node info")
	}

	return &nodeInfo{
		id:   nodeID,
		name: "fake-node-name",
		addr: "fake-node-addr",
	}, nil
}

func (c *nomadClientMock) Exec(ctx context.Context, alloc *api.Allocation, taskID string, command []string, stdout, stderr io.Writer) error {
	if _, err := io.WriteString(stdout, "stdout output"); err != nil {
		return err
	}

	if _, err := io.WriteString(stderr, "stderr output"); err != nil {
		return err
	}

	return nil
}

func Test_executor_exec(t *testing.T) {
	t.Parallel()

	e := executor{
		client: &nomadClientMock{},
		logger: log.WithField("logger", "test"),
	}

	alloc := &api.Allocation{
		ID:     "fake-allocation-id",
		NodeID: "fake-node-id",
	}

	out, err := e.exec(context.Background(), alloc, "fake-task", []string{"fake"})
	require.NoError(t, err)

	expectedOut := execOutput{
		allocID: "fake-allocation-id",
		node: &nodeInfo{
			id:   "fake-node-id",
			name: "fake-node-name",
			addr: "fake-node-addr",
		},
		stdout: "stdout output",
		stderr: "stderr output",
	}

	assert.Equal(t, expectedOut, *out)
}

func Test_nodeInfo_String(t *testing.T) {
	t.Parallel()

	nodeInfo := &nodeInfo{
		id:   "fake-node-id",
		name: "fake-node-name",
		addr: "fake-node-addr",
	}
	expected := "ID: fake-node-id, Name: fake-node-name, Addr: fake-node-addr"
	assert.Equal(t, expected, nodeInfo.String())
}
