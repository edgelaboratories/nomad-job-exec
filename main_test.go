package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/nomad/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	job = &api.Job{
		ID: stringToPtr("job-1"),
		TaskGroups: []*api.TaskGroup{
			{
				Name: stringToPtr("tg-1"),
				Tasks: []*api.Task{
					api.NewTask("task-1", "docker"),
					api.NewTask("task-2", "docker"),
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
			TaskGroup:    "tg-1",
			Job:          job,
		},
	}
)

type mockClient struct{}

func (c *mockClient) jobAllocations(jobID string) ([]*api.AllocationListStub, error) {
	if jobID != *job.ID {
		return nil, fmt.Errorf("failed to get allocatins for job %s", jobID)
	}

	return []*api.AllocationListStub{
		{
			ID:           "alloc-1",
			ClientStatus: "running",
		},
		{
			ID:           "alloc-2",
			ClientStatus: "running",
		},
	}, nil
}

func (c *mockClient) allocationInfo(allocID string) (*api.Allocation, error) {
	alloc, ok := allocs[allocID]
	if !ok {
		return nil, fmt.Errorf("failed to find allocation info for %s", allocID)
	}

	return alloc, nil
}

func (c *mockClient) allocationExec(ctx context.Context, alloc *api.Allocation, task string, cmd []string) (*execOutput, error) {
	return &execOutput{
		allocID: alloc.ID,
		stdout:  "stdout output",
		stderr:  "stderr output",
	}, nil
}

func Test_getAllocationsInfo(t *testing.T) {
	t.Parallel()

	infos, err := getAllocationsInfo(&mockClient{}, "job-1", "task-1")
	require.NoError(t, err)

	assert.Equal(t, 2, len(infos))

	assert.Equal(t, allocs["alloc-1"], infos[0].alloc)
	assert.Equal(t, "task-1", infos[0].task)

	assert.Equal(t, allocs["alloc-2"], infos[1].alloc)
	assert.Equal(t, "task-1", infos[1].task)
}

func Test_getAllocationInfo(t *testing.T) {
	t.Parallel()

	info, err := getAllocationInfo(&mockClient{}, "alloc-1", "task-1")
	require.NoError(t, err)

	assert.Equal(t, "task-1", info.task)
	assert.Equal(t, allocs["alloc-1"], info.alloc)

	info, err = getAllocationInfo(&mockClient{}, "alloc-2", "task-1")
	require.NoError(t, err)

	assert.Equal(t, "task-1", info.task)
	assert.Equal(t, allocs["alloc-2"], info.alloc)
}

func Test_getTaskName(t *testing.T) {
	t.Parallel()

	expectedName := "toto"

	alloc := &api.Allocation{
		TaskGroup: "tg",
		Job: &api.Job{
			TaskGroups: []*api.TaskGroup{
				{
					Name: stringToPtr("tg"),
					Tasks: []*api.Task{
						api.NewTask("toto", "docker"),
					},
				},
			},
		},
	}

	res, err := getTaskName(alloc)
	require.NoError(t, err)
	assert.Equal(t, expectedName, res)
}

func Test_getTaskName_MissingTaskGroup(t *testing.T) {
	t.Parallel()

	alloc := &api.Allocation{
		TaskGroup: "tg",
		Job: &api.Job{
			TaskGroups: []*api.TaskGroup{
				{
					Name: stringToPtr("tg-2"),
				},
			},
		},
	}

	_, err := getTaskName(alloc)
	assert.Error(t, err)
}

func Test_getTaskName_MultipleTasks(t *testing.T) {
	t.Parallel()

	alloc := &api.Allocation{
		TaskGroup: "tg",
		Job: &api.Job{
			TaskGroups: []*api.TaskGroup{
				{
					Name: stringToPtr("tg"),
					Tasks: []*api.Task{
						api.NewTask("toto", "docker"),
						api.NewTask("tata", "docker"),
					},
				},
			},
		},
	}

	_, err := getTaskName(alloc)
	assert.Error(t, err)
}

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
	assert.Equal(t, "stdout output", out.stdout)
	assert.Equal(t, "stderr output", out.stderr)
}

func stringToPtr(str string) *string {
	return &str
}
