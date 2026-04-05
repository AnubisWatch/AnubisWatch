# ADR-007: MCP Protocol for AI Integration

## Status

Accepted

## Context

AI agents (Claude Code, etc.) need to interact with AnubisWatch:
- Query monitor status
- Create/modify monitors via natural language
- Investigate incidents
- Get system statistics

## Decision

We implemented the **Model Context Protocol (MCP)** for AI agent integration.

### Architecture

```
┌─────────────────┐      MCP over HTTP      ┌─────────────────┐
│   AI Agent      │◄────────────────────────►│   MCP Server    │
│  (Claude Code)  │    POST /api/v1/mcp     │  (AnubisWatch)  │
└─────────────────┘                         └────────┬────────┘
                                                     │
                                            ┌────────┴────────┐
                                            │  Built-in Tools │
                                            │  - list_souls   │
                                            │  - get_soul     │
                                            │  - force_check  │
                                            │  - get_judgments│
                                            │  - list_incidents│
                                            │  - get_stats    │
                                            │  - create_soul  │
                                            └─────────────────┘
```

### MCP Methods Supported

| Method | Purpose |
|--------|---------|
| `initialize` | Handshake and capability negotiation |
| `tools/list` | List available tools |
| `tools/call` | Execute a tool |
| `resources/list` | List available resources |
| `resources/read` | Read a resource |
| `prompts/list` | List available prompts |
| `prompts/get` | Get a prompt template |

### Built-in Resources

- `anubis://docs/getting-started` - Setup guide
- `anubis://docs/api-reference` - API documentation
- `anubis://status/current` - Current system status

### Built-in Prompts

- `analyze-soul` - Get monitor analysis
- `incident-summary` - Summarize active incidents
- `create-monitor-guide` - Guide for creating monitors

## Consequences

### Positive
- AI agents can interact programmatically
- Natural language monitor management
- Consistent protocol across AI tools
- Extensible tool system

### Negative
- Additional authentication layer needed
- MCP protocol changes require updates
- Security considerations for AI actions

## Alternatives Considered

### REST API Only
- **Pros**: Simpler, standard HTTP
- **Cons**: No semantic understanding, harder for AI to discover
- **Rejected**: AI-native experience requirement

### GraphQL
- **Pros**: Flexible queries, introspection
- **Cons**: More complex, no AI-specific features
- **Rejected**: MCP provides better AI integration

### Custom Protocol
- **Pros**: Full control, tailored to needs
- **Cons**: No ecosystem support, AI tools wouldn't support
- **Rejected**: MCP is emerging standard
