package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	runtimePort      int
	runtimeLANAccess bool
	appRestartCh     chan struct{}
)

func setRuntimeState(port int, lanAccess bool) {
	runtimePort = port
	runtimeLANAccess = lanAccess
}

func setRestartChannel(ch chan struct{}) {
	appRestartCh = ch
}

func main() {
	portFlag := flag.Int("port", 0, "listening port (overrides config)")
	noGUI := flag.Bool("no-gui", false, "do not open GUI window")
	flag.Parse()

	for {
		cfg, err := LoadConfig()
		if err != nil {
			log.Printf("[config] failed to load: %v, using defaults", err)
			cfg = DefaultConfig()
		}

		if *portFlag > 0 {
			cfg.Port = *portFlag
		}
		if cfg.Port <= 0 || cfg.Port > 65535 {
			cfg.Port = 8188
		}

		initTrace()
		svc := InitAnalysisService(&cfg)
		if svc != nil {
			svc.config = &cfg
		}
		logger := NewLogger(2000)
		proxy := NewProxyServer(&cfg, logger)
		initWSHub(logger)

		// 决定监听的主机地址
		bindHost := "127.0.0.1"
		if cfg.LANAccess {
			bindHost = "0.0.0.0"
		}
		addr := fmt.Sprintf("%s:%d", bindHost, cfg.Port)

		ln, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatalf("[server] startup failed: failed to listen on %s: %v", addr, err)
		}

		server := &http.Server{
			Addr:    addr,
			Handler: proxy,
		}

		// 异步启动 HTTP 服务
		go func() {
			log.Printf("[server] DSPlus starting on %s", addr)
			log.Printf("[server] OpenAI upstream: %s", cfg.OpenAIUpstream)
			log.Printf("[server] Anthropic upstream: %s", cfg.AnthropicUpstream)
			if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
				log.Fatalf("[server] failed to start: %v", err)
			}
		}()

		// 异步打开 GUI
		shutdownCh := make(chan struct{})
		if !*noGUI && cfg.AutoOpenGUI {
			go openGUI(fmt.Sprintf("http://127.0.0.1:%d", cfg.Port), shutdownCh)
		}

		fmt.Printf("DSPlus v0.1.0\n")
		fmt.Printf("Listening on http://%s:%d\n", bindHost, cfg.Port)
		fmt.Printf("GUI: http://127.0.0.1:%d\n", cfg.Port)
		fmt.Printf("Press Ctrl+C or close GUI window to stop\n")

		setRuntimeState(cfg.Port, cfg.LANAccess)

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		restartCh := make(chan struct{}, 1)
		setRestartChannel(restartCh)

		select {
		case <-sigCh:
			log.Println("[server] shutting down via signal...")
			server.Shutdown(context.Background())
			return
		case <-shutdownCh:
			log.Println("[server] shutting down via GUI close...")
			server.Shutdown(context.Background())
			return
		case <-restartCh:
			log.Println("[server] restarting service...")
			server.Shutdown(context.Background())
			// 给一些时间端口释放
			time.Sleep(300 * time.Millisecond)
		}
	}
}
