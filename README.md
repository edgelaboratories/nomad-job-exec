# nomad-job-exec

`nomad-job-exec` is a CLI tool that allows to execute command on multiple Nomad allocations concurrently or sequentially.

## How it works

In a few words, `nomad-job-exec` does the following:

- list all the allocations for the given `--job`,
- find all the "valid" (running, that have an unambiguous task) allocations:
  - if `--task` was provided:
    - if the allocation doesn't have the provided task, skip it
    - else add it to the list of "valid" allocations
  - else: retrieve the tasks associated to the allocation
    - if more than one task is found, fail and ask to specify `--task`
    - else add the allocation to the list of "valid" allocations
- execute the command on all the "valid" allocations and dump the output to stdout/stderr

## Installation

**NB**: This project is still in development. Releases will be available once stabilized.

Download from the [Releases page](https://github.com/edgelaboratories/nomad-job-exec/releases) and put it somewhere in your `$PATH`.

## Usage

Define the `NOMAD_ADDR` and `NOMAD_TOKEN` environment variables and then execute a command on multiple Nomad allocations.

```shell
❯ nomad-job-exec --command "ps" --job $JOB_NAME
```

### Options

- In order to execute the command concurrently, use the flag `--parallel`
- To execute the command on a specific task only, specify it with the `--task` flag
- A default timeout is set to `10m` but can be changed with the flag `--timeout`

Use `--help` to get the details of all the optional flags available.
