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

func (c *apiClientWrap) jobAllocations(jobID, namespace string) ([]*api.AllocationListStub, error) {
	allocations, _, err := c.Jobs().Allocations(jobID, true, &api.QueryOptions{Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("failed to get the list of allocations for job %s (%s): %w", jobID, namespace, err)
	}

	return allocations, nil
}

func (c *apiClientWrap) allocationInfo(allocID, namespace string) (*api.Allocation, error) {
	alloc, _, err := c.Allocations().Info(allocID, &api.QueryOptions{Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve allocation %s (%s) info: %w", allocID, namespace, err)
	}

	return alloc, nil
}

type execOutput struct {
	allocID  string
	exitCode int
	stdout   string
	stderr   string
}

func (c *apiClientWrap) allocationExec(ctx context.Context, alloc *api.Allocation, task, namespace string, cmd []string) (*execOutput, error) {
	var bufStdout, bufStderr bytes.Buffer

	exitCode, err := c.Allocations().Exec(ctx, alloc, task, false, cmd, os.Stdin, &bufStdout, &bufStderr, nil, &api.QueryOptions{Namespace: namespace})
	if err != nil {
		return nil, fmt.Errorf("failed to exec command on allocation (%s): %w", namespace, err)
	}

	return &execOutput{
		allocID:  alloc.ID,
		exitCode: exitCode,
		stdout:   bufStdout.String(),
		stderr:   bufStderr.String(),
	}, nil
}
