package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const sshConfigMarker = "# broker-managed"

func sshConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ssh-config",
		Short: "Configure SSH for seamless access to broker clusters",
		Long: `Writes SSH config so that 'ssh <cluster>.broker' and VS Code Remote SSH work automatically.

This adds an Include directive to ~/.ssh/config pointing to a broker-managed
config file. Run this once -- new clusters are accessible immediately without
re-running this command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := installSSHConfig(); err != nil {
				return err
			}
			fmt.Println("SSH config installed.")
			fmt.Println("")
			fmt.Println("You can now SSH into any cluster:")
			fmt.Println("  ssh local.broker")
			fmt.Println("")
			fmt.Println("VS Code Remote SSH also works -- connect to <cluster>.broker")
			return nil
		},
	}
}

func installSSHConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	brokerDir := filepath.Join(home, ".broker")
	if err := os.MkdirAll(brokerDir, 0o755); err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}

	managedConfig := fmt.Sprintf(`%s
Host *.broker
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    LogLevel ERROR
    User root
    ProxyCommand %s ssh --stdio --hostname-suffix .broker %%h
`, sshConfigMarker, self)

	managedPath := filepath.Join(brokerDir, "ssh_config")
	if err := os.WriteFile(managedPath, []byte(managedConfig), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", managedPath, err)
	}

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}

	sshConfigPath := filepath.Join(sshDir, "config")

	existing, _ := os.ReadFile(sshConfigPath)
	includeLine := fmt.Sprintf("Include %s\n", managedPath)

	if strings.Contains(string(existing), managedPath) {
		return nil
	}

	updated := includeLine + string(existing)
	return os.WriteFile(sshConfigPath, []byte(updated), 0o644)
}

func sshConfigInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	managedPath := filepath.Join(home, ".broker", "ssh_config")
	_, err = os.Stat(managedPath)
	return err == nil
}
