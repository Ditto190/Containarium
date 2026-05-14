package main

import (
	"log"
	"os"

	"github.com/footprintai/containarium/internal/mcp"
	"github.com/footprintai/containarium/pkg/version"
)

func main() {
	// Log to stderr so stdout stays clean for the MCP protocol stream.
	// Set this BEFORE LoadConfig so any config-load logging lands on
	// stderr too (the printUsage path logs to whatever's wired up).
	log.SetOutput(os.Stderr)

	// Read configuration from environment or config file
	config := mcp.LoadConfig()

	if config.ServerURL == "" {
		printUsage()
		log.Fatal("CONTAINARIUM_SERVER_URL environment variable is required")
	}
	if config.JWTToken == "" && config.JWTTokenFile == "" {
		printUsage()
		log.Fatal("either CONTAINARIUM_JWT_TOKEN or CONTAINARIUM_JWT_TOKEN_FILE environment variable is required")
	}

	// Create MCP server with protobuf-defined contracts
	// All message types defined in proto/containarium/v1/mcp.proto
	server, err := mcp.NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create MCP server: %v", err)
	}

	log.Printf("Starting Containarium MCP Server (version %s, commit %s)",
		version.GetVersion(), version.GetCommitHash())
	log.Printf("Server URL: %s", config.ServerURL)
	log.Printf("Debug mode: %v", config.Debug)

	// Start MCP server (reads from stdin, writes to stdout)
	if err := server.Start(); err != nil {
		log.Fatalf("MCP server error: %v", err)
	}
}

// printUsage prints usage information and example configuration
func printUsage() {
	log.Println("")
	log.Println("=== Containarium MCP Server Configuration ===")
	log.Println("")
	log.Println("Required environment variables:")
	log.Println("  CONTAINARIUM_SERVER_URL      - URL of the Containarium REST API (e.g., http://localhost:8080)")
	log.Println("  CONTAINARIUM_JWT_TOKEN       - JWT token for authentication  (use one or the other)")
	log.Println("  CONTAINARIUM_JWT_TOKEN_FILE  - Path to a file with the JWT — re-read on every request,")
	log.Println("                                 so rotating the token is `mv newtoken oldpath`. Prefer this")
	log.Println("                                 for long-running MCP processes that need to survive rotation.")
	log.Println("")
	log.Println("Optional environment variables:")
	log.Println("  CONTAINARIUM_DEBUG           - Enable debug logging (true/false)")
	log.Println("  CONTAINARIUM_SENTINEL_HOST   - Public SSH endpoint, shown in create_container responses")
	log.Println("")
	log.Println("Example usage:")
	log.Println("  export CONTAINARIUM_SERVER_URL='http://localhost:8080'")
	log.Println("  export CONTAINARIUM_JWT_TOKEN='eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...'")
	log.Println("  /usr/local/bin/mcp-server")
	log.Println("")
	log.Println("Claude Desktop configuration (~/.config/claude/claude_desktop_config.json):")
	log.Println(`{`)
	log.Println(`  "mcpServers": {`)
	log.Println(`    "containarium": {`)
	log.Println(`      "command": "/usr/local/bin/mcp-server",`)
	log.Println(`      "env": {`)
	log.Println(`        "CONTAINARIUM_SERVER_URL": "http://your-server:8080",`)
	log.Println(`        "CONTAINARIUM_JWT_TOKEN": "your-jwt-token"`)
	log.Println(`      }`)
	log.Println(`    }`)
	log.Println(`  }`)
	log.Println(`}`)
	log.Println("")
}
