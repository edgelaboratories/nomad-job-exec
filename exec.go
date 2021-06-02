package main

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/nomad/api"
	log "github.com/sirupsen/logrus"
)

type executor struct {
	client       *api.Client
	queryOptions *api.QueryOptions
	logger       *log.Entry
}

func (e executor) exec(ctx context.Context, allocID, taskID string, cmd []string) (*execOutput, error) {
	e.logger.Infof("retrieving allocation info")

	alloc, _, err := e.client.Allocations().Info(allocID, e.queryOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve allocation info for %s: %w", allocID, err)
	}

	e.logger.Infof("getting info for node %s", alloc.NodeID)

	node, _, err := e.client.Nodes().Info(alloc.NodeID, e.queryOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to get the node info for id %s: %w", alloc.NodeID, err)
	}

	e.logger.Info("executing command")

	var bufStdout, bufStderr bytes.Buffer

	_, err = e.client.Allocations().Exec(ctx, alloc, taskID, false, cmd, os.Stdin, &bufStdout, &bufStderr, nil, e.queryOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to exec command on allocation %s: %w", alloc.ID, err)
	}

	e.logger.Info("command executed")

	return &execOutput{
		allocID: alloc.ID,
		node: &nodeInfo{
			id:   node.ID,
			name: node.Name,
			addr: node.HTTPAddr,
		},
		stdout: bufStdout.String(),
		stderr: bufStderr.String(),
	}, nil
}

type execOutput struct {
	allocID string
	node    *nodeInfo
	stdout  string
	stderr  string
}

type nodeInfo struct {
	id   string
	name string
	addr string
}

func (n nodeInfo) String() string {
	return fmt.Sprintf("ID: %s, Name: %s, Addr: %s", n.id, n.name, n.addr)
}
