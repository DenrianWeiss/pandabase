# Model Context Protocol (MCP) Integration

Pandabase supports the Model Context Protocol (MCP), allowing AI agents like Claude Desktop to search your knowledge base directly.

## Capabilities

Currently, the Pandabase MCP server provides the following tools:

- `search`: Search for documents in the knowledge base using semantic (vector) and full-text search.
- `ingest`: Direct text content ingestion into a specific namespace.
- `import_url`: Request the server to fetch and index content from a public URL (supports web or Notion).

## Transport Modes

Pandabase offers two ways to use MCP: **Stdio** (recommended for local use) and **HTTP/SSE** (integrated into the main server).

---

### 1. Stdio Mode (Recommended)

Stdio mode is the easiest way to connect Claude Desktop to your local Pandabase instance.

#### Configuration for Claude Desktop

Add the following to your `claude_desktop_config.json` file:

```json
{
  "mcpServers": {
    "pandabase": {
      "command": "/usr/local/bin/go",
      "args": [
        "run",
        "/absolute/path/to/pandabase/cmd/mcp/main.go",
        "-mode",
        "stdio"
      ],
      "env": {
        "CONFIG_PATH": "/absolute/path/to/pandabase/config.yaml"
      }
    }
  }
}
```

---

### 2. HTTP Mode (Authenticated)

Pandabase also provides an MCP interface over HTTP, integrated directly into the main application. This mode requires authentication via a **Persistent Access Token**.

#### A. Create a Persistent Access Token

1.  Log in to the Pandabase Web UI.
2.  Go to **Settings** (or your user profile).
3.  Look for **API Tokens** or **Personal Access Tokens**.
4.  Create a new token (e.g., named "Claude MCP").
5.  **Copy the token immediately**; it starts with `pdb_`. This is your persistent access token.

#### B. Connection Endpoints

The MCP endpoints are available under the main server URL (usually `http://localhost:8080`):

- **SSE Endpoint**: `http://localhost:8080/api/v1/mcp/sse`
- **Messages Endpoint**: `http://localhost:8080/api/v1/mcp/messages`

#### C. Configuration for AI Agents (Remote/HTTP)

For agents that support MCP over HTTP, you must provide the `Authorization` header:

```text
Authorization: Bearer pdb_your_token_here
```

---

## Usage in AI Agent

Once configured, the AI agent will have access to the `search` tool. You can ask it questions like:

- "Search my knowledge base for information about project X"
- "What do our documents say about our vacation policy?"
- "Find the technical specifications for the new API"

The agent will automatically use the `search` tool to retrieve relevant context and use it to answer your questions.

## How to Build the CLI Tool

If you prefer using a binary instead of `go run`:

```bash
go build -o pandabase-mcp ./cmd/mcp/main.go
```

## Prerequisites

- A running Pandabase instance (via `docker-compose up -d` or `go run cmd/server/main.go`).
- A valid `config.yaml` with embedding API key configured.
- Documents already ingested into your namespaces.
