package main

import (
	"testing"

	"github.com/hashicorp/nomad/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_getTaskName(t *testing.T) {
	t.Parallel()

	expectedName := "toto"

	alloc := &api.Allocation{
		TaskGroup: "tg1",
		Job: &api.Job{
			TaskGroups: []*api.TaskGroup{
				{
					Name: stringToPtr("tg1"),
					Tasks: []*api.Task{
						api.NewTask(expectedName, "docker"),
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
		TaskGroup: "tg1",
		Job: &api.Job{
			TaskGroups: []*api.TaskGroup{
				{
					Name: stringToPtr("tg2"),
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
		TaskGroup: "tg1",
		Job: &api.Job{
			TaskGroups: []*api.TaskGroup{
				{
					Name: stringToPtr("tg1"),
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

func stringToPtr(str string) *string {
	return &str
}
