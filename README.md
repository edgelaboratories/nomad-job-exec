# nomad-job-exec

nomad-job-exec is a CLI tool that allows to execute command on multiple Nomad allocations concurrently or sequentially.

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
