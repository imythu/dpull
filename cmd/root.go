package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/imythu/dpull/internal/app"
	"github.com/imythu/dpull/internal/cache"
	"github.com/imythu/dpull/internal/completion"
	"github.com/imythu/dpull/internal/crane"
	"github.com/imythu/dpull/internal/docker"
	"github.com/imythu/dpull/internal/logger"
	"github.com/imythu/dpull/internal/proxy"
	"github.com/imythu/dpull/internal/runner"
	"github.com/spf13/cobra"
)

// Version is set at build time with -ldflags "-X github.com/imythu/dpull/cmd.Version=...".
var Version = "dev"

func Execute(ctx context.Context) int {
	root, err := cache.DefaultRoot()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	commandRunner := runner.ExecRunner{}
	manager := cache.New(root)
	application := &app.Application{
		Cache:  manager,
		Crane:  &crane.Client{CacheDir: manager.BlobDir(), Progress: os.Stdout},
		Docker: docker.Client{Runner: commandRunner},
		Log:    logger.New(os.Stdout),
		Now:    time.Now,
	}
	proxyResolver, err := proxy.DefaultResolver()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	command := NewRootCommand(application, proxyResolver)
	command.SetContext(ctx)
	if err := command.Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func NewRootCommand(application *app.Application, resolvers ...*proxy.Resolver) *cobra.Command {
	var options app.Options
	resolver := firstResolver(resolvers)
	command := &cobra.Command{
		Use:           "dpull [IMAGE...]",
		Short:         "Pull container images with built-in crane and load them into Docker",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		Version:       Version,
		RunE: func(command *cobra.Command, args []string) error {
			proxyInfo, err := resolver.Resolve(options.Proxy)
			if err != nil {
				return fmt.Errorf("resolve proxy: %w", err)
			}
			options.Images = append([]string(nil), args...)
			options.Proxy = proxyInfo.Effective
			options.ProxySource = string(proxyInfo.Source)
			if err := application.Run(command.Context(), options); err != nil {
				return fmt.Errorf("run dpull: %w", err)
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.BoolVar(&options.Up, "up", false, "run docker compose up -d after loading images")
	flags.StringVarP(&options.ComposeFile, "file", "f", "", "specify a Compose file")
	flags.StringVar(&options.Proxy, "proxy", "", "proxy URL for image downloads (http://, https://, socks5://), or \"direct\" to disable")
	flags.StringVar(&options.Platform, "platform", "", "image platform as os/arch[/variant] (default: Docker daemon platform)")
	flags.BoolVar(&options.Keep, "keep", false, "keep downloaded image archives")
	command.AddCommand(newProxyCommand(resolver))
	command.AddCommand(newCleanCommand(application))
	command.AddCommand(newCompletionCommand())
	return command
}

func newCompletionCommand() *cobra.Command {
	command := &cobra.Command{
		Use:       "completion SHELL",
		Short:     "Generate or install shell completion",
		ValidArgs: completion.Supported,
		Args:      cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if err := generateCompletion(command.Root(), args[0], command.OutOrStdout()); err != nil {
				return fmt.Errorf("generate completion: %w", err)
			}
			return nil
		},
	}
	command.AddCommand(newCompletionInstallCommand())
	return command
}

func newCompletionInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:       "install [SHELL]",
		Short:     "Install completion for the current shell",
		ValidArgs: completion.Supported,
		Args:      cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			shell, err := completionShell(args)
			if err != nil {
				return err
			}
			var script bytes.Buffer
			if err := generateCompletion(command.Root(), shell, &script); err != nil {
				return fmt.Errorf("generate %s completion: %w", shell, err)
			}
			path, err := completion.DefaultPath(shell)
			if err != nil {
				return err
			}
			if err := completion.Install(path, script.Bytes()); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Installed %s completion to %s\n", shell, path)
			printCompletionHint(command, shell, path)
			return nil
		},
	}
}

func completionShell(args []string) (string, error) {
	if len(args) == 1 {
		if err := completion.Validate(args[0]); err != nil {
			return "", err
		}
		return args[0], nil
	}
	shell, err := completion.Detect(os.Getenv)
	if err != nil {
		return "", err
	}
	return shell, nil
}

func generateCompletion(root *cobra.Command, shell string, output io.Writer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletion(output)
	case "zsh":
		return root.GenZshCompletion(output)
	case "fish":
		return root.GenFishCompletion(output, true)
	case "powershell":
		return root.GenPowerShellCompletion(output)
	default:
		return completion.Validate(shell)
	}
}

func printCompletionHint(command *cobra.Command, shell, path string) {
	switch shell {
	case "zsh":
		_, _ = fmt.Fprintln(command.OutOrStdout(), "Ensure ~/.zfunc is in fpath, then restart zsh.")
	case "powershell":
		_, _ = fmt.Fprintf(command.OutOrStdout(), "Add `. %s` to your PowerShell profile, then restart PowerShell.\n", path)
	default:
		_, _ = fmt.Fprintln(command.OutOrStdout(), "Restart the shell to enable completion.")
	}
}

func newCleanCommand(application *app.Application) *cobra.Command {
	return &cobra.Command{
		Use:     "clean",
		Aliases: []string{"cleanup"},
		Short:   "Remove all downloaded image caches",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			removed, err := application.Clean()
			if err != nil {
				return fmt.Errorf("clean downloads: %w", err)
			}
			_, _ = fmt.Fprintln(command.OutOrStdout(), "[Cleanup]")
			_, _ = fmt.Fprintf(command.OutOrStdout(), "  %-10s %d\n", "Removed:", removed)
			_, _ = fmt.Fprintf(command.OutOrStdout(), "  %-10s %s\n", "Cache:", application.Cache.Root)
			return nil
		},
	}
}

func firstResolver(resolvers []*proxy.Resolver) *proxy.Resolver {
	if len(resolvers) > 0 && resolvers[0] != nil {
		return resolvers[0]
	}
	resolver, err := proxy.DefaultResolver()
	if err != nil {
		return &proxy.Resolver{LookupEnv: os.LookupEnv}
	}
	return resolver
}

func newProxyCommand(resolver *proxy.Resolver) *cobra.Command {
	command := &cobra.Command{
		Use:   "proxy",
		Short: "Show the effective proxy configuration",
		Long: "Show the effective crane proxy and every configuration source.\n\n" +
			"Supported proxy URL schemes: http://, https://, and socks5://.\n" +
			"Use \"direct\" at any level (or `dpull proxy disable`) to turn the proxy off.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			info, err := resolver.Resolve("")
			if err != nil {
				return fmt.Errorf("resolve proxy: %w", err)
			}
			printProxyInfo(command, info)
			return nil
		},
	}
	command.AddCommand(newProxySetCommand(resolver))
	command.AddCommand(newProxyDisableCommand(resolver))
	command.AddCommand(newProxyResetCommand(resolver))
	return command
}

func newProxySetCommand(resolver *proxy.Resolver) *cobra.Command {
	var global bool
	command := &cobra.Command{
		Use:   "set URL",
		Short: "Save the proxy configuration",
		Long: "Save an HTTP, HTTPS, or SOCKS5 proxy URL. Supported formats:\n" +
			"  http://host:port\n  https://host:port\n  socks5://host:port\n  direct (disable the proxy)",
		Example: "  dpull proxy set http://127.0.0.1:7890\n" +
			"  dpull proxy set socks5://127.0.0.1:1080\n" +
			"  sudo dpull proxy set -g https://proxy.example.com:8443",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			path, err := resolver.Set(args[0], global)
			if err != nil {
				return fmt.Errorf("save proxy: %w", err)
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Proxy saved to %s\n", path)
			return nil
		},
	}
	command.Flags().BoolVarP(&global, "global", "g", false, "save to /etc/dpull.conf")
	return command
}

func newProxyDisableCommand(resolver *proxy.Resolver) *cobra.Command {
	var global bool
	command := &cobra.Command{
		Use:     "disable",
		Aliases: []string{"off"},
		Short:   "Turn the proxy off and connect to registries directly",
		Long: "Save proxy=direct so downloads connect directly. This overrides the builtin\n" +
			"default and lower-priority configs; DPULL_PROXY and --proxy still take precedence.\n" +
			"Re-enable with `dpull proxy set URL` or `dpull proxy reset`.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			path, err := resolver.Set(proxy.Direct, global)
			if err != nil {
				return fmt.Errorf("disable proxy: %w", err)
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Proxy disabled in %s\n", path)
			return printEffectiveProxy(command, resolver)
		},
	}
	command.Flags().BoolVarP(&global, "global", "g", false, "save to /etc/dpull.conf")
	return command
}

func newProxyResetCommand(resolver *proxy.Resolver) *cobra.Command {
	var global bool
	command := &cobra.Command{
		Use:   "reset",
		Short: "Remove the saved proxy setting and fall back to lower-priority sources",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			path, removed, err := resolver.Reset(global)
			if err != nil {
				return fmt.Errorf("reset proxy: %w", err)
			}
			if removed {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "Removed %s\n", path)
			} else {
				_, _ = fmt.Fprintf(command.OutOrStdout(), "No proxy config at %s\n", path)
			}
			return printEffectiveProxy(command, resolver)
		},
	}
	command.Flags().BoolVarP(&global, "global", "g", false, "remove /etc/dpull.conf")
	return command
}

func printEffectiveProxy(command *cobra.Command, resolver *proxy.Resolver) error {
	info, err := resolver.Resolve("")
	if err != nil {
		return fmt.Errorf("resolve proxy: %w", err)
	}
	_, _ = fmt.Fprintf(command.OutOrStdout(), "Now using: %s\n", info.Describe())
	return nil
}

func printProxyInfo(command *cobra.Command, info proxy.Info) {
	status := "enabled"
	value := info.Effective
	if !info.Enabled() {
		status = "disabled"
		value = "none (direct connection)"
	}
	_, _ = fmt.Fprintln(command.OutOrStdout(), "[Proxy]")
	_, _ = fmt.Fprintf(command.OutOrStdout(), "  %-12s %s\n", "Status:", status)
	_, _ = fmt.Fprintf(command.OutOrStdout(), "  %-12s %s\n", "Effective:", value)
	_, _ = fmt.Fprintf(command.OutOrStdout(), "  %-12s %s\n", "Source:", info.Source)
	_, _ = fmt.Fprintln(command.OutOrStdout(), "\n[Sources]")
	_, _ = fmt.Fprintf(command.OutOrStdout(), "  %-13s %s\n", "Builtin:", displayProxy(info.Builtin))
	_, _ = fmt.Fprintf(command.OutOrStdout(), "  %-13s %s (%s)\n", "System:", displayProxy(info.System), proxy.SystemConfig)
	_, _ = fmt.Fprintf(command.OutOrStdout(), "  %-13s %s (%s)\n", "User:", displayProxy(info.User), info.UserPath)
	_, _ = fmt.Fprintf(command.OutOrStdout(), "  %-13s %s (%s)\n", "Environment:", displayProxy(info.Env), proxy.EnvironmentName)
}

func displayProxy(value string) string {
	if value == "" {
		return "not set"
	}
	return value
}
