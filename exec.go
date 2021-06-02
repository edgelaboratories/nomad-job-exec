package main

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/hashicorp/nomad/api"
	log "github.com/sirupsen/logrus"
)

type executor struct {
	client interface {
		NodeInfo(nodeID string) (*nodeInfo, error)
		Exec(ctx context.Context, alloc *api.Allocation, taskID string, command []string, stdout, stderr io.Writer) error
	}
	logger *log.Entry
}

type nodeInfo struct {
	id   string
	name string
	addr string
}

func (n nodeInfo) String() string {
	return fmt.Sprintf("ID: %s, Name: %s, Addr: %s", n.id, n.name, n.addr)
}

func (e executor) exec(ctx context.Context, alloc *api.Allocation, taskID string, cmd []string) (*execOutput, error) {
	e.logger.Infof("getting info for node %s", alloc.NodeID)

	nodeInfo, err := e.client.NodeInfo(alloc.NodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get the node info for id %s: %w", alloc.NodeID, err)
	}

	e.logger.Info("executing command")

	var bufStdout, bufStderr bytes.Buffer

	if err := e.client.Exec(ctx, alloc, taskID, cmd, &bufStdout, &bufStderr); err != nil {
		return nil, fmt.Errorf("failed to exec command on allocation %s: %w", alloc.ID, err)
	}

	e.logger.Info("command executed")

	return &execOutput{
		allocID: alloc.ID,
		node:    nodeInfo,
		stdout:  bufStdout.String(),
		stderr:  bufStderr.String(),
	}, nil
}

type execOutput struct {
	allocID string
	node    *nodeInfo
	stdout  string
	stderr  string
}
