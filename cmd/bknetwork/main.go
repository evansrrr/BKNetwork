package main

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"bknetwork/internal/server"

	"github.com/kardianos/service"
)

var logger service.Logger

func main() {
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

	svcConfig := &service.Config{
		Name:        "BKNetwork",
		DisplayName: "BKNetwork Service",
		Description: "Background network helper serving a local web UI on localhost:13335",
	}

	prg := &program{}
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

type program struct {
	httpSrv *server.Server
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
