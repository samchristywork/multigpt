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

```
Usage: multigpt [flags]
  --config        Path to config file (overrides default search paths)
  --context       Thread context across questions (sequential)
  --dry-run       Print resolved config and questions without sending requests
  --format        Output format: plain, tsv, or json (default: plain)
  -j              Max concurrent requests (0 = unlimited)
  --list-models   List available models and exit
  --max-tokens    Maximum tokens to generate (-1 for server default)
  --model         Ollama model to use (comma-separated for multiple)
  --no-stats      Omit per-answer token/timing stats from output
  --output        Write results to file instead of stdout
  --quiet         Suppress progress output on stderr
  --retries       Number of retries on transient errors
  --role          System prompt (use | to assign different roles per model)
  --stream        Stream tokens as they arrive (plain format only)
  --template      Go template wrapping each input line
  --think         Enable think mode
  --timeout       HTTP timeout in seconds per query (default: 120)
  --url           Ollama server URL (default: http://localhost:11434)
  --version       Print version and exit
  --completion    Print shell completion script (bash, zsh, fish)
```

## Configuration

## Examples

## Dependencies

## License

This work is licensed under the GNU General Public License version 3 (GPLv3).

[<img src="https://s-christy.com/status-banner-service/GPLv3_Logo.svg" width="150" />](https://www.gnu.org/licenses/gpl-3.0.en.html)
