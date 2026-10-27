// Command bknetwork runs the local HTTP API as a Windows service, or in the
// foreground for debugging. The desktop UI is the Tauri application: this
// binary never opens a browser and has no tray icon of its own.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bknetwork/internal/handlers"
	"bknetwork/internal/logging"
	"bknetwork/internal/server"
	appsettings "bknetwork/internal/settings"

	"github.com/kardianos/service"
)

var logger service.Logger

func main() {
	// A Windows service has no console either, so the log file is the only
	// place its output can be read afterwards.
	if path, logErr := logging.Setup(); logErr != nil {
		log.Printf("file logging disabled: %v", logErr)
	} else if service.Interactive() {
		log.Printf("logging to %s", path)
	}

	cfg, err := appsettings.Load()
	if err != nil {
		log.Printf("failed to load settings: %v", err)
		cfg = appsettings.Settings{}
	}

	svcConfig := &service.Config{
		Name:        "BKNetwork",
		DisplayName: "BKNetwork Service",
		Description: "Background network helper serving a local HTTP API on 127.0.0.1:13335",
	}

	prg := &program{settings: cfg}
	svc, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			if err := svc.Install(); err != nil {
				log.Fatalf("install error: %v", err)
			}
			log.Println("service installed")
			return
		case "uninstall":
			if err := svc.Uninstall(); err != nil {
				log.Fatalf("uninstall error: %v", err)
			}
			log.Println("service uninstalled")
			return
		case "run":
			// Run in the foreground below.
		case appsettings.StartupNoElevateArg:
			// The desktop shell used to relaunch itself elevated with this flag.
			// It is gone; the flag is only meaningful for the Tauri executable.
			log.Printf("%s is not supported by this binary; start the Tauri app instead", appsettings.StartupNoElevateArg)
			return
		default:
			log.Printf("ignoring unknown argument %q (supported: install, uninstall, run)", os.Args[1])
		}
	}

	if service.Interactive() {
		if err := runConsole(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	if runErr := svc.Run(); runErr != nil {
		if logger != nil {
			logger.Error(runErr)
			return
		}
		log.Fatal(runErr)
	}
}

// runConsole serves the API in the foreground until an interrupt, which is how
// the service is debugged without installing it.
func runConsole(cfg appsettings.Settings) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := server.NewServer("")
	go func() {
		if err := srv.Start(ctx); err != nil {
			log.Println("server error:", err)
			stop()
		}
	}()
	startWarpIfEnabled(cfg)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

type program struct {
	httpSrv  *server.Server
	settings appsettings.Settings
}

func (p *program) Start(s service.Service) error {
	// Start must not block, so the server runs in its own goroutine.
	p.httpSrv = server.NewServer("")
	go func() {
		if err := p.httpSrv.Start(context.Background()); err != nil {
			if logger != nil {
				logger.Error(err)
				return
			}
			log.Println("server error:", err)
		}
	}()
	startWarpIfEnabled(p.settings)
	return nil
}

func (p *program) Stop(s service.Service) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if p.httpSrv != nil {
		_ = p.httpSrv.Shutdown(ctx)
	}
	return nil
}

func startWarpIfEnabled(cfg appsettings.Settings) {
	if !cfg.WarpAutoStart {
		return
	}
	go func() {
		if err := handlers.StartWarp(); err != nil {
			if logger != nil {
				logger.Warning(err)
				return
			}
			log.Printf("warp auto start failed: %v", err)
		}
	}()
}
