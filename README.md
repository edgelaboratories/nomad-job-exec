# nomad-alloc-exec

nomad-alloc-exec is a CLI tool that allows to execute command on multiple Nomad allocations concurrently or sequentially.

## Installation

**NB**: This project is still in development. Releases will be available once stabilized.

Download from the [Releases page](https://github.com/edgelaboratories/nomad-alloc-exec/releases) and put it somewhere in your `$PATH`.

## Usage

Define the `NOMAD_ADDR` and `NOMAD_TOKEN` environment variables and then execute a command on multiple Nomad allocations.

```shell
❯ nomad-alloc-exec --command "ps" --job $JOB_NAME --task $TASK_NAME
```
