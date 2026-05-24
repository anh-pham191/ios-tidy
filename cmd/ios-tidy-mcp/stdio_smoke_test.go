//go:build smoke

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestServerInitializeRoundTrip(t *testing.T) {
	binPath, _ := filepath.Abs("../../bin/ios-tidy-mcp")
	if _, err := os.Stat(binPath); err != nil {
		t.Skipf("binary not built: %v (run `make build-mcp`)", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer cmd.Process.Kill()

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"1"}}}` + "\n"
	if _, err := io.WriteString(stdin, req); err != nil {
		t.Fatalf("write: %v", err)
	}

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v (stderr: %s)", err, stderr.String())
	}
	var resp struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("unmarshal: %v (line: %s)", err, line)
	}
	if resp.Result.ProtocolVersion == "" {
		t.Error("expected protocolVersion in response")
	}
	if resp.Result.ServerInfo.Name != "ios-tidy-mcp" {
		t.Errorf("server name = %q, want ios-tidy-mcp", resp.Result.ServerInfo.Name)
	}
}
