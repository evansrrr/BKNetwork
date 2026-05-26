package main

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"bknetwork/internal/handlers"
	"bknetwork/internal/server"
	appsettings "bknetwork/internal/settings"

	"github.com/kardianos/service"
)

var logger service.Logger

func main() {
	if !hasStartupNoElevateArg() {
		relaunched, err := ensureElevatedAtStartup()
		if err != nil {
			if errors.Is(err, errElevationCanceled) {
				log.Println("UAC 提权已取消，程序退出")
				return
			}
			log.Fatalf("failed to request administrator privileges: %v", err)
		}
		if relaunched {
			return
		}
	}

	cfg, err := appsettings.Load()
	if err != nil {
		log.Printf("failed to load settings: %v", err)
		cfg = appsettings.Settings{}
	}
	if err := appsettings.ApplyStartupShortcut(cfg.AutoStart); err != nil {
		log.Printf("failed to sync autostart setting: %v", err)
	}

	svcConfig := &service.Config{
		Name:        "BKNetwork",
		DisplayName: "BKNetwork Service",
		Description: "Background network helper serving a local web UI on localhost:13335",
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
			// fallthrough to run in console
		}
	}

	if err := service.Control(svc, "status"); err == nil {
		// likely running as a service manager
	}

	if service.Interactive() {
		if err := runDesktopApp(); err != nil {
			log.Fatal(err)
		}
		return
	}

	error := svc.Run()
	if error != nil {
		if logger != nil {
			logger.Error(error)
		} else {
			log.Fatal(error)
		}
	}
}

func hasStartupNoElevateArg() bool {
	for _, arg := range os.Args[1:] {
		if arg == appsettings.StartupNoElevateArg {
			return true
		}
	}
	return false
}

type program struct {
	httpSrv  *server.Server
	settings appsettings.Settings
}

func (p *program) Start(s service.Service) error {
	// Start should not block. Start the server in a goroutine.
	ctx := context.Background()
	p.httpSrv = server.NewServer("")
	go func() {
		if err := p.httpSrv.Start(ctx); err != nil {
			if logger != nil {
				logger.Error(err)
			} else {
				log.Println("server error:", err)
			}
		}
	}()
	if p.settings.WarpAutoStart {
		go func() {
			if err := handlers.StartWarp(); err != nil {
				if logger != nil {
					logger.Warning(err)
				} else {
					log.Printf("warp auto start failed: %v", err)
				}
			}
		}()
	}
	return nil
}

func (p *program) Stop(s service.Service) error {
	// Stop should stop the server gracefully.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p.httpSrv != nil {
		_ = p.httpSrv.Shutdown(ctx)
	}
	return nil
}
