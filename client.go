package main

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/nomad/api"
)

type apiClientWrap struct {
	*api.Client
}

func (c *apiClientWrap) jobAllocations(jobID string) ([]*api.AllocationListStub, error) {
	allocations, _, err := c.Jobs().Allocations(jobID, true, &api.QueryOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get the list of allocations for job %s: %w", jobID, err)
	}

	return allocations, nil
}

func (c *apiClientWrap) allocationInfo(allocID string) (*api.Allocation, error) {
	alloc, _, err := c.Allocations().Info(allocID, &api.QueryOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve allocation %s info: %w", allocID, err)
	}

	return alloc, nil
}

type execOutput struct {
	allocID string
	stdout  string
	stderr  string
}

func (c *apiClientWrap) allocationExec(ctx context.Context, alloc *api.Allocation, task string, cmd []string) (*execOutput, error) {
	var bufStdout, bufStderr bytes.Buffer

	exitCode, err := c.Allocations().Exec(ctx, alloc, task, false, cmd, os.Stdin, &bufStdout, &bufStderr, nil, &api.QueryOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to exec command on allocation: %w", err)
	}

	output := &execOutput{
		allocID: alloc.ID,
		stdout:  bufStdout.String(),
		stderr:  bufStderr.String(),
	}

	if exitCode != 0 {
		err = fmt.Errorf("command exited with code: %d", exitCode)
	} else {
		err = nil
	}

	return output, err
}
