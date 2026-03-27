![Banner](https://s-christy.com/sbs/status-banner.svg?icon=action/model_training&hue=220&title=MultiGPT&description=Concurrent%20multi-model%20Ollama%20CLI)

## Overview

MultiGPT is a command-line tool for sending questions to local Ollama models.
It reads questions from a file or stdin and dispatches them concurrently across
one or more models, collecting and formatting the results. It is written in Go
and has no external dependencies beyond the standard library.

By specifying a comma-separated list of models, you can fan out the same
questions to multiple models simultaneously and compare their responses side by
side. The tool supports plain text, TSV, and JSON output formats, streaming
mode, conversation context threading, and Go template expansion for prompt
construction.

## Features

- Send questions to one or more Ollama models in a single invocation
- Concurrent execution with configurable parallelism limit
- Multiple output formats: plain text, TSV, and JSON
- Streaming mode that prints tokens as they arrive
- Conversation context threading across sequential questions
- Go template support for constructing prompts from input lines
- Per-question timeout overrides using a `<duration>: <question>` prefix
- Think mode for reasoning-capable models
- Retry with exponential backoff on transient errors
- Config file with layered precedence (file -> env -> flags)
- Shell completion scripts for bash, zsh, and fish
- Dry-run mode to preview resolved config and questions

## Usage

## Configuration

## Examples

## Dependencies

## License

This work is licensed under the GNU General Public License version 3 (GPLv3).

[<img src="https://s-christy.com/status-banner-service/GPLv3_Logo.svg" width="150" />](https://www.gnu.org/licenses/gpl-3.0.en.html)
