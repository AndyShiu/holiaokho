// Command holiaokho is the Holiaokho repository manager server.
//
//	holiaokho [--config config.yaml]                 run the server
//	holiaokho import-nexus --nexus-db ... --nexus-blobs ... [--link] [--dry-run]
//	holiaokho backup --out backup.tar.gz [--with-blobs]
//	holiaokho restore --in backup.tar.gz
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

	"io"

	"github.com/holiaokho/holiaokho/internal/backup"
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
		// Kept in the tree but disabled until the migration path is finalised.
		if os.Getenv("HOLIAOKHO_ENABLE_NEXUS_IMPORT") != "1" {
			fmt.Fprintln(os.Stderr, "import-nexus is disabled in this build; set HOLIAOKHO_ENABLE_NEXUS_IMPORT=1 to use it")
			os.Exit(2)
		}
		runImport(args)
	case "backup":
		runBackup(args)
	case "restore":
		runRestore(args)
	case "version":
		fmt.Println("holiaokho", server.Version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", sub)
		os.Exit(2)
	}
}

func setup(fs *flag.FlagSet, args []string) (config.Config, *slog.Logger, *server.System) {
	cfgPath := fs.String("config", os.Getenv("HOLIAOKHO_CONFIG"), "path to config.yaml")
	fs.Parse(args)
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	log, sys := server.NewSystemLogger(cfg.Log.Level, cfg.Log.Format)
	return cfg, log, sys
}

func runServe(args []string) {
	cfg, log, sys := setup(flag.NewFlagSet("serve", flag.ExitOnError), args)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv, err := server.New(ctx, cfg, log, sys)
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
	cfg, log, sys := setup(fs, args)
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
	srv, err := server.New(ctx, cfg, log, sys)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	if err := nexusimport.Run(ctx, srv.Content, srv.Auth, log, opt); err != nil {
		log.Error("import failed", "err", err)
		os.Exit(1)
	}
}

func runBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	out := fs.String("out", "", "output file (default: stdout)")
	withBlobs := fs.Bool("with-blobs", false, "include blob contents (large)")
	cfg, log, sys := setup(fs, args)
	ctx := context.Background()
	srv, err := server.New(ctx, cfg, log, sys)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	var w io.Writer = os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			log.Error("create", "err", err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}
	logf := func(f string, a ...any) { log.Info(fmt.Sprintf(f, a...)) }
	if err := backup.Write(ctx, srv.Content, server.Version, *withBlobs, w, logf); err != nil {
		log.Error("backup failed", "err", err)
		os.Exit(1)
	}
}

func runRestore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	in := fs.String("in", "", "backup archive (default: stdin)")
	cfg, log, sys := setup(fs, args)
	ctx := context.Background()
	srv, err := server.New(ctx, cfg, log, sys)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	var r io.Reader = os.Stdin
	if *in != "" {
		f, err := os.Open(*in)
		if err != nil {
			log.Error("open", "err", err)
			os.Exit(1)
		}
		defer f.Close()
		r = f
	}
	logf := func(f string, a ...any) { log.Info(fmt.Sprintf(f, a...)) }
	if err := backup.Restore(ctx, srv.Content, r, logf); err != nil {
		log.Error("restore failed", "err", err)
		os.Exit(1)
	}
}
