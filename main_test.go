package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/hashicorp/nomad/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_getAllocationsInfo(t *testing.T) {
	t.Parallel()

	// Target first task of first task group
	info, err := getAllocationsInfo(&mockClient{}, "job-1", "task-11", "foo")
	require.NoError(t, err)

	require.Len(t, info, 1)
	assert.Equal(t, allocs["alloc-1"], info[0].alloc)
	assert.Equal(t, "task-11", info[0].task)

	// Target second task of first task group
	info, err = getAllocationsInfo(&mockClient{}, "job-1", "task-12", "foo")
	require.NoError(t, err)

	require.Len(t, info, 1)
	assert.Equal(t, allocs["alloc-1"], info[0].alloc)
	assert.Equal(t, "task-12", info[0].task)

	// Target first task of second task group
	info, err = getAllocationsInfo(&mockClient{}, "job-1", "task-21", "foo")
	require.NoError(t, err)

	require.Len(t, info, 2)

	for _, i := range info {
		assert.Equal(t, allocs[i.alloc.ID], i.alloc)
		assert.Equal(t, "task-21", i.task)
	}
}

func Test_getAllocationsInfo_FailWhenTaskIsAmbiguous(t *testing.T) {
	t.Parallel()

	_, err := getAllocationsInfo(&mockClient{}, "job-1", "", "foo")
	require.Error(t, err)
}

func Test_getAllocationsInfo_FilterOnTask(t *testing.T) {
	t.Parallel()

	info, err := getAllocationsInfo(&mockClient{}, "job-1", "inexistant-task", "foo")
	require.NoError(t, err)

	assert.Empty(t, info)
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

func Test_executeSequentially(t *testing.T) {
	t.Parallel()

	info, err := getAllocationsInfo(&mockClient{}, "job-1", "task-21", "foo")
	require.NoError(t, err)

	ctx := context.Background()
	assert.NoError(t, executeSequentially(
		ctx,
		slog.Default().With(slog.Bool("test", true)),
		&mockClient{},
		info,
		"foo",
		[]string{"ls"},
	))
}

func Test_executeSequentially_Failure(t *testing.T) {
	t.Parallel()

	info, err := getAllocationsInfo(&mockClient{}, "job-1", "task-21", "foo")
	require.NoError(t, err)

	ctx := context.Background()
	assert.Error(t, executeSequentially(
		ctx,
		slog.Default().With(slog.Bool("test", true)),
		&mockClient{
			failExec: true,
		},
		info,
		"foo",
		[]string{"ls"},
	))
}

func Test_executeConcurrently(t *testing.T) {
	t.Parallel()

	info, err := getAllocationsInfo(&mockClient{}, "job-1", "task-21", "foo")
	require.NoError(t, err)

	ctx := context.Background()
	assert.NoError(t, executeConcurrently(
		ctx,
		slog.Default().With(slog.Bool("test", true)),
		&mockClient{},
		info,
		"foo",
		[]string{"ls"},
		5,
	))
}

func Test_executeConcurrently_Failure(t *testing.T) {
	t.Parallel()

	info, err := getAllocationsInfo(&mockClient{}, "job-1", "task-21", "foo")
	require.NoError(t, err)

	ctx := context.Background()
	assert.Error(t, executeConcurrently(
		ctx,
		slog.Default().With(slog.Bool("test", true)),
		&mockClient{
			failExec: true,
		},
		info,
		"foo",
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
