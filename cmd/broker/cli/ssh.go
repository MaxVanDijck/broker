package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/coder/websocket"
	"github.com/spf13/cobra"
)

func sshCmd() *cobra.Command {
	var (
		user           string
		port           int
		stdio          bool
		hostnameSuffix string
		extraArgs      []string
	)

	cmd := &cobra.Command{
		Use:   "ssh CLUSTER",
		Short: "SSH into a cluster node",
		Long: `Open an SSH session to a cluster node.

Run 'broker ssh-config' once to enable seamless SSH:

  ssh my-cluster.broker
  
VS Code Remote SSH also works -- connect to <cluster>.broker`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]

			if hostnameSuffix != "" {
				clusterName = strings.TrimSuffix(clusterName, hostnameSuffix)
			}

			ensureServer()

			if stdio {
				return runStdioProxy(clusterName)
			}

			// Interactive SSH: use ProxyCommand through ourselves so all
			// traffic goes via the server tunnel. No direct TCP to nodes.
			self, err := os.Executable()
			if err != nil {
				self = "broker"
			}

			sshArgs := []string{
				"-t",
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"-o", "LogLevel=ERROR",
				"-o", fmt.Sprintf("Port=%d", port),
				"-o", fmt.Sprintf("ProxyCommand=%s ssh --stdio %s", self, clusterName),
			}

			if user != "" {
				sshArgs = append(sshArgs, "-l", user)
			}

			sshArgs = append(sshArgs, extraArgs...)
			sshArgs = append(sshArgs, clusterName)

			sshBin, err := exec.LookPath("ssh")
			if err != nil {
				return fmt.Errorf("ssh not found in PATH")
			}

			return syscall.Exec(sshBin, append([]string{"ssh"}, sshArgs...), os.Environ())
		},
	}

	cmd.Flags().StringVarP(&user, "user", "l", "root", "SSH user")
	cmd.Flags().IntVarP(&port, "port", "p", 2222, "SSH port (agent default: 2222)")
	cmd.Flags().BoolVar(&stdio, "stdio", false, "Proxy SSH traffic over stdin/stdout (for ProxyCommand)")
	cmd.Flags().StringVar(&hostnameSuffix, "hostname-suffix", "", "Strip this suffix from the hostname (used by ProxyCommand)")
	cmd.Flags().StringArrayVarP(&extraArgs, "ssh-flag", "o", nil, "Extra SSH flags")

	cmd.Flags().MarkHidden("hostname-suffix")

	return cmd
}

// runStdioProxy connects to the server's SSH proxy WebSocket and relays
// stdin/stdout. All SSH traffic goes through the server tunnel to the
// agent's built-in SSH server. No direct TCP connection to the node.
func runStdioProxy(clusterName string) error {
	addr := serverAddr()
	wsURL := strings.Replace(strings.Replace(addr, "https://", "wss://", 1), "http://", "ws://", 1)
	url := fmt.Sprintf("%s/api/v1/clusters/%s/ssh", wsURL, clusterName)

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to ssh proxy: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	conn.SetReadLimit(1 << 20)

	var wg sync.WaitGroup
	wg.Add(2)

	// stdin -> WebSocket
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if writeErr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket -> stdout
	go func() {
		defer wg.Done()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			os.Stdout.Write(data)
		}
	}()

	wg.Wait()
	return nil
}
