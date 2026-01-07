# apicli

A command-line HTTP client written in Go for making API requests, managing request history, and organizing requests into collections.

## Features

- **HTTP Requests**: Make GET, POST, PUT, PATCH, and DELETE requests with custom headers and body data
- **Endpoint Aliases**: Create shortcuts for frequently used base URLs (e.g., `api` → `https://api.example.com`)
- **Request History**: Automatically track and browse your request history
- **Collections**: Organize related requests into collections and run them as a batch
- **Color Output**: Pretty-printed JSON responses with color-coded status indicators
- **File Support**: Load request bodies from files using `@filename` syntax

## Installation

### Prerequisites

- Go 1.22 or later (for local build)
- Docker and Docker Compose (for containerized usage)

### Build from Source

```bash
# Clone the repository
git clone https://github.com/yourusername/apicli.git
cd apicli

# Build the binary
go build -o apicli .

# Optionally, move to a directory in your PATH
mv apicli /usr/local/bin/
```

### Using Docker

```bash
# Build the Docker image
docker build -t apicli .

# Run with Docker Compose (recommended - persists history/collections)
docker-compose run --rm apicli <command>
```

## Usage

### Making HTTP Requests

```bash
# GET request
apicli get https://api.example.com/users

# POST request with JSON body
apicli post https://api.example.com/users -d '{"name": "John", "email": "john@example.com"}'

# PUT request with body from file
apicli put https://api.example.com/users/1 -d @body.json

# DELETE request
apicli delete https://api.example.com/users/1

# Request with custom headers
apicli get https://api.example.com/users -H "Authorization: Bearer token" -H "Accept: application/json"

# Verbose mode (show response headers)
apicli get https://api.example.com/users -v
```

### Endpoint Aliases

Create shortcuts for frequently used base URLs to simplify your requests.

```bash
# Create an alias
apicli alias create myapi https://api.example.com/v1

# Now use the alias in requests
apicli get myapi/users              # Expands to https://api.example.com/v1/users
apicli post myapi/users -d '{"name": "John"}'

# List all aliases
apicli alias list

# Show a specific alias
apicli alias show myapi

# Delete an alias
apicli alias delete myapi
```

**Example workflow with Star Wars API:**
```bash
# Create alias
apicli alias create starwars https://www.swapi.tech/api

# Use the alias
apicli get starwars/people/1
apicli get starwars/planets/3
```

### Request History

All requests are automatically saved to history (up to 100 entries).

```bash
# List recent requests
apicli history

# Show last 5 requests
apicli history -n 5

# Show details of a specific request by index
apicli history show 1

# Clear all history
apicli history clear
```

### Collections

Organize related requests into named collections for easy reuse.

```bash
# List all collections
apicli collection list

# Create a new collection
apicli collection create my-api

# Add a request to a collection
apicli collection add my-api "Get Users" GET https://api.example.com/users

# Add a POST request with headers and body
apicli collection add my-api "Create User" POST https://api.example.com/users \
  -H "Content-Type: application/json" \
  -d '{"name": "John"}'

# Show all requests in a collection
apicli collection show my-api

# Run all requests in a collection
apicli collection run my-api

# Delete a collection
apicli collection delete my-api
```

## Configuration

Data is stored in `~/.apicli/`:
- `history.json` - Request history
- `collections.json` - Saved collections
- `aliases.json` - Endpoint aliases

When using Docker, mount a volume to persist data:
```bash
docker-compose run --rm apicli <command>
# Data is persisted to ./data/ directory
```

## Project Structure

```
.
├── main.go                 # Entry point
├── cmd/                    # CLI commands (Cobra)
│   ├── root.go            # Root command and global flags
│   ├── request.go         # HTTP method commands
│   ├── alias.go           # Endpoint alias management
│   ├── collection.go      # Collection management
│   └── history.go         # History commands
├── internal/              # Internal packages
│   ├── model/             # Data structures
│   ├── http/              # HTTP client wrapper
│   ├── format/            # Output formatting
│   └── storage/           # JSON file persistence
├── Dockerfile             # Multi-stage Docker build
└── docker-compose.yml     # Docker Compose configuration
```

## Dependencies

- [cobra](https://github.com/spf13/cobra) - CLI framework
- [color](https://github.com/fatih/color) - Colorized output
- [uuid](https://github.com/google/uuid) - Unique identifiers

## License

MIT
