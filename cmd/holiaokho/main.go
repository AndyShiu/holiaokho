// Command holiaokho is the Holiaokho repository manager server.
//
//	holiaokho [--config config.yaml]                 run the server
//	holiaokho import-nexus --nexus-db ... --nexus-blobs ... [--link] [--dry-run]
//	holiaokho version
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/holiaokho/holiaokho/internal/config"
	"github.com/holiaokho/holiaokho/internal/nexusimport"
	"github.com/holiaokho/holiaokho/internal/server"
)

func main() {
	args := os.Args[1:]
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "", "serve":
		runServe(args)
	case "import-nexus":
		runImport(args)
	case "version":
		fmt.Println("holiaokho", server.Version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", sub)
		os.Exit(2)
	}
}

func setup(fs *flag.FlagSet, args []string) (config.Config, *slog.Logger) {
	cfgPath := fs.String("config", os.Getenv("HOLIAOKHO_CONFIG"), "path to config.yaml")
	fs.Parse(args)
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		level = slog.LevelInfo
	}
	var h slog.Handler
	if cfg.Log.Format == "json" {
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	} else {
		h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	return cfg, slog.New(h)
}

func runServe(args []string) {
	cfg, log := setup(flag.NewFlagSet("serve", flag.ExitOnError), args)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv, err := server.New(ctx, cfg, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	if err := srv.Run(ctx); err != nil {
		log.Error("server error", "err", err)
		os.Exit(1)
	}
}

func runImport(args []string) {
	fs := flag.NewFlagSet("import-nexus", flag.ExitOnError)
	var opt nexusimport.Options
	var formats string
	fs.StringVar(&opt.NexusDB, "nexus-db", os.Getenv("NEXUS_DB_URL"), "Nexus PostgreSQL URL (postgres://user:pass@host:5432/nexus)")
	fs.StringVar(&opt.BlobsDir, "nexus-blobs", "", "path to the Nexus blobs directory (<nexus-data>/blobs)")
	fs.BoolVar(&opt.Link, "link", false, "hardlink blob files instead of copying (same filesystem only)")
	fs.BoolVar(&opt.DryRun, "dry-run", false, "report what would be imported without writing")
	fs.BoolVar(&opt.NoContent, "no-content", false, "import configuration, users and policies only")
	fs.BoolVar(&opt.NoUsers, "no-users", false, "skip users and roles")
	fs.StringVar(&formats, "formats", "maven2,npm,docker", "comma-separated Nexus formats to import content for")
	cfg, log := setup(fs, args)
	if opt.NexusDB == "" {
		fmt.Fprintln(os.Stderr, "--nexus-db is required")
		os.Exit(2)
	}
	if !opt.NoContent && opt.BlobsDir == "" {
		fmt.Fprintln(os.Stderr, "--nexus-blobs is required unless --no-content")
		os.Exit(2)
	}
	for _, f := range strings.Split(formats, ",") {
		if f = strings.TrimSpace(f); f != "" {
			opt.Formats = append(opt.Formats, f)
		}
	}
	ctx := context.Background()
	srv, err := server.New(ctx, cfg, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	if err := nexusimport.Run(ctx, srv.Content, srv.Auth, log, opt); err != nil {
		log.Error("import failed", "err", err)
		os.Exit(1)
	}
}
