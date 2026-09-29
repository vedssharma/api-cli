# apicli

A command-line HTTP client written in Go for making API requests, managing request history, and organizing requests into collections you can run, assert on, and share.

## Features

- **HTTP Requests**: GET, POST, PUT, PATCH, DELETE, HEAD and OPTIONS with headers, query parameters, JSON, form and multipart bodies, and bearer/basic auth shortcuts
- **Scripting friendly**: `--raw`, `--select` (JSON path), `--output`, `--silent`, `--fail`, meaningful exit codes
- **Environments & variables**: `{{variable}}` placeholders, named environments, values captured from responses
- **Endpoint Aliases**: Shortcuts for frequently used base URLs (e.g., `api` → `https://api.example.com`)
- **Request History**: Automatic history with search and replay
- **Collections**: Group requests, edit them, and run them as a batch with response assertions
- **Import / Export**: Postman, curl, HAR and OpenAPI/Swagger in; Postman and curl out
- **Shell completion**: including your collection, environment and alias names
- **Safe by default**: secrets in headers are redacted before anything is stored, and cloud metadata endpoints are blocked

## Installation

### Prerequisites

- Go 1.22 or later (for local build)
- Docker and Docker Compose (for containerized usage)

### Build from Source

```bash
git clone https://github.com/vedssharma/api-cli.git
cd api-cli

# Build the binary
go build -o apicli .

# Optionally embed a version (shown by `apicli --version`)
go build -ldflags "-X api/cmd.version=1.0.0" -o apicli .

# Optionally, move to a directory in your PATH
mv apicli /usr/local/bin/
```

### Using Docker

```bash
# Build the Docker image
docker build -t apicli .

# Persist data in ./data (the container runs as uid 1000)
mkdir -p data && sudo chown 1000 data
docker-compose run --rm apicli <command>
```

### Shell completion

```bash
# bash (add to ~/.bashrc), zsh, fish and powershell are also supported
source <(apicli completion bash)
```

Completion suggests collection, environment and alias names, history IDs, and the `--collection` / `--env` flag values.

## Usage

### Making HTTP Requests

```bash
# GET request
apicli get https://api.example.com/users

# POST request with JSON body
apicli post https://api.example.com/users -d '{"name": "John", "email": "john@example.com"}'

# PUT request with body from a file (must be inside the current directory)
apicli put https://api.example.com/users/1 -d @body.json

# HEAD / OPTIONS
apicli head https://api.example.com/users
apicli options https://api.example.com/users

# Custom headers, and verbose mode (show response headers)
apicli get https://api.example.com/users -H "Accept: application/json" -v
```

#### Query parameters, bodies and auth

```bash
# Query parameters (URL-encoded for you)
apicli get https://api.example.com/search -q term="hello world" -q page=2

# Auth shortcuts (an explicit -H "Authorization: ..." always wins)
apicli get https://api.example.com/me --bearer "$TOKEN"
apicli get https://api.example.com/me --basic user:password

# URL-encoded form body
apicli post https://api.example.com/login --form user=jo --form password=secret

# Multipart form with a file upload (files must be inside the current directory)
apicli post https://api.example.com/upload -F title=Cat -F photo=@cat.png
```

Only one of `-d`, `--form` and `--multipart` may be used per request.

#### Connection options

```bash
apicli get https://api.example.com --timeout 5        # seconds (default 30)
apicli get https://api.example.com/old --no-redirect  # show the 3xx instead of following it
apicli get https://api.example.com --proxy http://localhost:8888
apicli get https://self-signed.test --insecure        # skip TLS verification (prints a warning)
```

#### Controlling output and exit codes

```bash
# Body only, no status line or formatting
apicli get https://api.example.com/users --raw

# Extract a value with a simple JSON path (.key, .key[0].key)
apicli get https://api.example.com/users --select .data[0].name

# Save the body to a file (0600), print just the status line
apicli get https://api.example.com/report -o report.json

# Print nothing; use the exit code
apicli get https://api.example.com/health --silent --fail && echo healthy

# Show the equivalent curl command instead of sending
apicli post https://api.example.com/users -d '{"a":1}' --bearer "$TOKEN" --curl
```

| Exit code | Meaning |
|-----------|---------|
| 0 | Success (any HTTP status, unless `--fail`) |
| 1 | Request or usage error, or a failed collection run |
| 22 | `--fail` was given and the status was 400 or higher |

Repeated response headers such as `Set-Cookie` are shown joined with `, `.

### Environments and Variables

Use `{{name}}` placeholders in URLs, headers, query parameters, bodies and form fields. They are replaced when a request is sent; an undefined variable is an error.

```bash
apicli env create dev
apicli env set dev base=https://api.dev.example.com token=abc123
apicli env use dev

apicli get '{{base}}/users' -H 'Authorization: Bearer {{token}}'

# Override for one run, or pick another environment
apicli get '{{base}}/users' --var base=http://localhost:8080
apicli get '{{base}}/users' --env prod

# Capture a value from the response into the environment
apicli post '{{base}}/login' -d '{"user":"jo"}' --capture token=.data.token

apicli env list          # * marks the active environment
apicli env show [name]
apicli env unset dev token
apicli env use --clear
apicli env delete dev
```

**History and collections store the placeholders, never their values**, so tokens kept in variables are not written there. Variables are stored in `~/.apicli/apicli.db` in plain text (file mode 0600) — treat that file like any credentials file.

### Endpoint Aliases

```bash
apicli alias create myapi https://api.example.com/v1
apicli get myapi/users              # Expands to https://api.example.com/v1/users

apicli alias list
apicli alias show myapi
apicli alias delete myapi
```

### Request History

All requests are saved to history (up to 100 entries). Sensitive headers are stored as `[REDACTED]` (see [Security](#security)).

```bash
apicli history                  # recent requests, newest first
apicli history -n 5
apicli history show 1           # by index or by ID

# Search by URL/method text, method and status (404 or a class like 4xx)
apicli history search users
apicli history search --status 4xx
apicli history search --method POST --status 201

# Send a stored request again (indexes from list and search both work)
apicli history replay 3
apicli history replay 3 --bearer "$TOKEN"     # re-supply redacted headers
apicli history replay 3 --env prod --no-history

apicli history clear
```

Redacted headers cannot be replayed, so they are dropped with a warning; supply them with `-H`, `--bearer` or `--basic`, or store them as a `{{variable}}` in the original request (a sensitive header whose value is only variables, like `Bearer {{token}}`, is kept as written).

### Collections

```bash
apicli collection list
apicli collection create my-api
apicli collection add my-api "Get Users" GET '{{base}}/users' -H 'Authorization: Bearer {{token}}'
apicli collection add my-api "Create User" POST '{{base}}/users' -d '{"name": "John"}' \
  --assert status=201 --assert .id
apicli collection show my-api         # numbered; shows assertions
apicli collection delete my-api

# Rename, edit and remove
apicli collection rename my-api users-api
apicli collection edit users-api 2 --url '{{base}}/v2/users' -H 'X-Trace: 1' -H 'Accept:'
apicli collection edit users-api "Create User" --assert status=201 --assert .id   # replaces assertions
apicli collection edit users-api "Create User" --clear-assert
apicli collection remove-request users-api 1        # by position (from `show`) or exact name

# Save the request you just sent (headers are redacted first)
apicli post https://api.example.com/users -d '{"a":1}' -c my-api
```

`collection edit` changes only the fields you pass; `-H 'Name: value'` adds or replaces a header and `-H 'Name:'` removes it.

#### Running a collection

```bash
apicli collection run my-api
apicli collection run my-api --env staging --fail --stop-on-error
apicli collection run my-api --capture token=.data.token   # feed later requests
```

The run exits `1` if any request fails, a variable is undefined, or an assertion fails. `--fail` also counts 4xx/5xx responses as failures and `--stop-on-error` stops at the first failure. `--capture name=.path` is tried against every response in the run and made available to the requests after it. `--timeout`, `--no-redirect`, `--proxy` and `--insecure` work here too.

#### Assertions

An assertion is `<target><operator><value>`:

| Target | Example |
|--------|---------|
| `status` | `status=200`, `status<400` |
| `body` | `body~"welcome"` |
| `header:Name` | `header:Content-Type~json` |
| `.json.path` | `.data.id=5`, `.items[0].name=Jo`, `.data.id` (exists) |

Operators: `=` `!=` `~` (contains) `!~` (does not contain) `<` `>` `<=` `>=` (numeric). Values may contain `{{variables}}`.

### Import and Export

#### Postman

```bash
apicli export postman                                   # all collections, to stdout
apicli export postman --collection my-api --output my-api.postman.json
apicli export postman --history

apicli import postman my-api.postman_collection.json
apicli import postman my-api.postman_collection.json --collection staging
```

Postman v2.0/v2.1 files are supported. Nested folders are flattened (`Auth / Login`); imported requests are appended if the collection already exists.

#### curl

```bash
# Quote the whole command, paste it after `curl`, or read it from stdin
apicli import curl "curl -X POST https://api.example.com/users -H 'Accept: text/plain' -d '{\"a\":1}'"
apicli import curl -c my-api curl https://api.example.com/users -H 'Accept: text/plain'
pbpaste | apicli import curl -

apicli export curl --collection my-api
apicli export curl --history --output replay.sh
```

Put apicli's own flags (`-c`, `-n`) before an unquoted command. Options that can't be represented (such as `-F` uploads) are reported and skipped. Sensitive headers are redacted on import.

#### HAR (browser network logs)

```bash
apicli import har session.har --filter /api/ --collection my-api
```

Browser-added headers (`Host`, `sec-*`, ...) are dropped and sensitive headers redacted; non-HTTP entries such as WebSockets are skipped.

#### OpenAPI / Swagger

```bash
apicli import openapi petstore.yaml                    # OpenAPI 3.x or Swagger 2.0, JSON or YAML
apicli import openapi api.json --base-url http://localhost:8080
apicli import openapi api.json --base-url '{{baseUrl}}'   # keep the host in an environment variable
```

One request is created per operation. Path parameters and required query/header parameters become `{{variables}}`; security schemes become placeholder credentials such as `Authorization: Bearer {{token}}` or `{{apiKey}}`; JSON bodies are sketched from the schema or example; and the first documented 2xx status becomes a `status=` assertion, so `collection run` doubles as a smoke test. The command prints the variables you need to set.

## Security

- **Header redaction**: `Authorization`, `Cookie`, API-key and cloud-credential headers are stored as `[REDACTED]` in history and collections, except when the value is only `{{variables}}`.
- **Body warning**: a warning is printed when a body looks like it contains a password or token; use `--no-history` to skip storing it. Response bodies are stored as received.
- **`@file` bodies** and multipart uploads may only read files inside the current directory (symlinks are resolved and checked).
- **Cloud metadata endpoints** (AWS, GCP, Azure, Alibaba, including IPv6 and redirected connections) are refused. Requests to localhost and private addresses are allowed with a warning.
- Only `http` and `https` URLs are accepted; terminal escape sequences in responses are neutralised before printing.
- Data files are created with owner-only permissions (`~/.apicli` is 0700, the database 0600).

## Configuration

Data is stored in a SQLite database at `~/.apicli/apicli.db` (history, collections, aliases, environments). Data from older versions that used `history.json`, `collections.json` and `aliases.json` is imported automatically on first run, and those files are renamed to `*.migrated`.

## Development

```bash
go build ./...
go vet ./...
gofmt -l .        # should print nothing
go test ./...
```

CI (`.github/workflows/ci.yml`) runs the same checks on Go 1.22 and the latest release.

## Project Structure

```
.
├── main.go                 # Entry point
├── cmd/                    # CLI commands (Cobra)
│   ├── root.go             # Root command, global flags, version
│   ├── request.go          # HTTP method commands and request pipeline
│   ├── options.go          # Query, form/multipart, auth and connection flags
│   ├── env.go              # Environments, variables and response capture
│   ├── alias.go            # Endpoint alias management
│   ├── collection*.go      # Collections: manage, edit, run, assertions
│   ├── history.go          # History list, show, search, replay
│   ├── import*.go          # Postman, curl, HAR and OpenAPI import
│   ├── export.go, curl.go  # Postman and curl export
│   └── completion.go       # Dynamic shell completion
├── internal/
│   ├── model/              # Data structures
│   ├── http/               # HTTP client, URL and address checks
│   ├── format/             # Output formatting and JSON path selection
│   ├── vars/               # {{variable}} substitution
│   ├── assert/             # Response assertions
│   ├── curl/               # curl command parser/builder
│   ├── har/                # HAR converter
│   ├── openapi/            # OpenAPI / Swagger converter
│   └── storage/            # SQLite persistence
├── Dockerfile              # Multi-stage Docker build
└── docker-compose.yml      # Docker Compose configuration
```

## Dependencies

- [cobra](https://github.com/spf13/cobra) - CLI framework
- [color](https://github.com/fatih/color) - Colorized output
- [uuid](https://github.com/google/uuid) - Unique identifiers
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) - Pure-Go SQLite driver (no CGO)
- [yaml.v3](https://github.com/go-yaml/yaml) - YAML parsing for OpenAPI import

## License

MIT
