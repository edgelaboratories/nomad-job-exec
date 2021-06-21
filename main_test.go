package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/nomad/api"
	log "github.com/sirupsen/logrus"
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

type mockClient struct{}

func (c *mockClient) jobAllocations(jobID string) ([]*api.AllocationListStub, error) {
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

	// Target first task of first task group
	info, err := getAllocationsInfo(&mockClient{}, "job-1", "task-11")
	require.NoError(t, err)

	require.Len(t, info, 1)
	assert.Equal(t, allocs["alloc-1"], info[0].alloc)
	assert.Equal(t, "task-11", info[0].task)

	// Target second task of first task group
	info, err = getAllocationsInfo(&mockClient{}, "job-1", "task-12")
	require.NoError(t, err)

	require.Len(t, info, 1)
	assert.Equal(t, allocs["alloc-1"], info[0].alloc)
	assert.Equal(t, "task-12", info[0].task)

	// Target first task of second task group
	info, err = getAllocationsInfo(&mockClient{}, "job-1", "task-21")
	require.NoError(t, err)

	require.Len(t, info, 2)

	for _, i := range info {
		assert.Equal(t, allocs[i.alloc.ID], i.alloc)
		assert.Equal(t, "task-21", i.task)
	}
}

func Test_getAllocationsInfo_FailWhenTaskIsAmbiguous(t *testing.T) {
	t.Parallel()

	_, err := getAllocationsInfo(&mockClient{}, "job-1", "")
	require.Error(t, err)
}

func Test_getAllocationsInfo_FilterOnTask(t *testing.T) {
	t.Parallel()

	info, err := getAllocationsInfo(&mockClient{}, "job-1", "inexistant-task")
	require.NoError(t, err)

	assert.Len(t, info, 0)
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

	res, err := retrieveTask(alloc)
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

	_, err := retrieveTask(alloc)
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

	_, err := retrieveTask(alloc)
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

func Test_allocationExec_Failure(t *testing.T) {
	t.Parallel()

	out, err := allocationExec(
		context.Background(),
		&mockClient{},
		&allocInfo{
			alloc: allocs["alloc-1"],
			task:  "task-1",
		},
		[]string{"idontexist"},
	)
	require.Error(t, err)

	assert.Equal(t, "alloc-1", out.allocID)
	assert.Equal(t, "stdout output", out.stdout)
	assert.Equal(t, "stderr output", out.stderr)
}

func Test_executeSequentially(t *testing.T) {
	t.Parallel()

	info, err := getAllocationsInfo(&mockClient{}, "job-1", "task-21")
	require.NoError(t, err)

	ctx := context.Background()
	assert.NoError(t, executeSequentially(
		ctx,
		log.WithField("test", true),
		&mockClient{},
		info,
		[]string{"ls"},
	))
}

func Test_executeConcurrently(t *testing.T) {
	t.Parallel()

	info, err := getAllocationsInfo(&mockClient{}, "job-1", "task-21")
	require.NoError(t, err)

	ctx := context.Background()
	assert.NoError(t, executeConcurrently(
		ctx,
		log.WithField("test", true),
		&mockClient{},
		info,
		[]string{"ls"},
		5,
	))
}

func Test_contains(t *testing.T) {
	t.Parallel()

	c := []string{"alpha", "beta"}

	assert.True(t, contains(c, "alpha"))
	assert.True(t, contains(c, "beta"))
	assert.False(t, contains(c, "gamma"))
}

func stringToPtr(str string) *string {
	return &str
}
