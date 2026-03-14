package deploy

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func Run(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("missing deploy target")
	}

	switch args[0] {
	case "vps":
		return runVPS(args[1:], stdout, stderr)
	case "client":
		return runClient(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unsupported deploy target %q", args[0])
	}
}

func runVPS(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("deploy vps", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "deploy/vps.yaml", "path to VPS deployment YAML")
	writeOnly := fs.Bool("write-only", false, "render files but do not build or restart services")

	if err := fs.Parse(args); err != nil {
		return err
	}

	spec, err := LoadVPSConfig(*configPath)
	if err != nil {
		return err
	}
	if *writeOnly {
		spec.WriteOnly = true
	}

	result, err := InstallVPS(spec)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "VPS deployment complete\n")
	_, _ = fmt.Fprintf(stdout, "  env: %s\n", result.EnvPath)
	_, _ = fmt.Fprintf(stdout, "  users: %s\n", result.UsersPath)
	_, _ = fmt.Fprintf(stdout, "  binary: %s\n", result.BinaryPath)
	_, _ = fmt.Fprintf(stdout, "  service: %s\n", result.ServicePath)
	return nil
}

func runClient(args []string, stdout io.Writer, stderr io.Writer) error {
	fs := flag.NewFlagSet("deploy client", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "deploy/client.yaml", "path to client deployment YAML")
	writeOnly := fs.Bool("write-only", false, "render files but do not reload or restart services")

	if err := fs.Parse(args); err != nil {
		return err
	}

	spec, err := LoadClientConfig(*configPath)
	if err != nil {
		return err
	}
	if *writeOnly {
		spec.WriteOnly = true
	}

	result, err := InstallClient(spec)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Client deployment complete\n")
	if len(result.ServicePaths) > 1 {
		for index, servicePath := range result.ServicePaths {
			_, _ = fmt.Fprintf(stdout, "  tunnel service %d: %s\n", index+1, servicePath)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "  tunnel service: %s\n", result.ServicePath)
	}
	if len(result.EnvPaths) > 1 {
		_, _ = fmt.Fprintf(stdout, "  default env file: %s\n", result.EnvPath)
		for index, envPath := range result.EnvPaths {
			_, _ = fmt.Fprintf(stdout, "  endpoint env %d: %s\n", index+1, envPath)
		}
	} else {
		_, _ = fmt.Fprintf(stdout, "  env file: %s\n", result.EnvPath)
	}
	_, _ = fmt.Fprintf(stdout, "  wrapper: %s\n", result.WrapperPath)
	return nil
}

func printUsage(w io.Writer) {
	_, _ = io.WriteString(w, `Usage:
  codex-gateway deploy vps [-config deploy/vps.yaml] [--write-only]
  codex-gateway deploy client [-config deploy/client.yaml] [--write-only]

Targets:
  vps     Render runtime files, build the binary, and install the local VPS service
  client  Render the SSH tunnel service and a proxy wrapper script on the client
`)
}

func ensureOutput(w io.Writer) io.Writer {
	if w == nil {
		return os.Stdout
	}
	return w
}
