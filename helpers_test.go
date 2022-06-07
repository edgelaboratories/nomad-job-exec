package main

import (
	"context"
	"fmt"

	"github.com/hashicorp/nomad/api"
)

var (
	job = &api.Job{
		ID: stringToPtr("job-1"),
		TaskGroups: []*api.TaskGroup{
			{
				Name: stringToPtr("tg-1"),
				Tasks: []*api.Task{
					api.NewTask("task-11", "docker"),
					api.NewTask("task-12", "docker"),
				},
			},
			{
				Name: stringToPtr("tg-2"),
				Tasks: []*api.Task{
					api.NewTask("task-21", "docker"),
				},
			},
		},
	}

	allocs = map[string]*api.Allocation{
		"alloc-1": {
			ID:           "alloc-1",
			ClientStatus: "running",
			TaskGroup:    "tg-1",
			Job:          job,
		},
		"alloc-2": {
			ID:           "alloc-2",
			ClientStatus: "running",
			TaskGroup:    "tg-2",
			Job:          job,
		},
		"alloc-3": {
			ID:           "alloc-3",
			ClientStatus: "complete",
			TaskGroup:    "tg-2",
			Job:          job,
		},
		"alloc-4": {
			ID:           "alloc-4",
			ClientStatus: "running",
			TaskGroup:    "tg-2",
			Job:          job,
		},
	}
)

type mockClient struct {
	failExec bool
}

func (c *mockClient) jobAllocations(jobID, namespace string) ([]*api.AllocationListStub, error) {
	if jobID != *job.ID {
		return nil, fmt.Errorf("failed to get allocatins for job %s", jobID)
	}

	res := []*api.AllocationListStub{}
	for _, alloc := range allocs {
		res = append(res, stubFromAlloc(alloc))
	}

	return res, nil
}

func stubFromAlloc(alloc *api.Allocation) *api.AllocationListStub {
	return &api.AllocationListStub{
		ID:           alloc.ID,
		ClientStatus: alloc.ClientStatus,
	}
}

func (c *mockClient) allocationInfo(allocID, namespace string) (*api.Allocation, error) {
	alloc, ok := allocs[allocID]
	if !ok {
		return nil, fmt.Errorf("failed to find allocation info for %s", allocID)
	}

	return alloc, nil
}

func (c *mockClient) allocationExec(ctx context.Context, alloc *api.Allocation, task, namespace string, cmd []string) (*execOutput, error) {
	if c.failExec {
		return &execOutput{
			allocID:  alloc.ID,
			exitCode: 1,
			stdout:   "stdout output",
			stderr:   "stderr output",
		}, nil
	}

	return &execOutput{
		allocID:  alloc.ID,
		exitCode: 0,
		stdout:   "stdout output",
		stderr:   "stderr output",
	}, nil
}

func stringToPtr(str string) *string {
	return &str
}
