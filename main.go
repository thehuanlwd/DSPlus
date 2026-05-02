package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	portFlag := flag.Int("port", 0, "listening port (overrides config)")
	noGUI := flag.Bool("no-gui", false, "do not open GUI window")
	flag.Parse()

	cfg, err := LoadConfig()
	if err != nil {
		log.Printf("[config] failed to load: %v, using defaults", err)
		cfg = DefaultConfig()
	}

	initTrace()

	if *portFlag > 0 {
		cfg.Port = *portFlag
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		cfg.Port = 8188
	}

	logger := NewLogger(2000)
	currentConfig = &cfg
	currentLogger = logger

	proxy := NewProxyServer(&cfg, logger)
	initWSHub(logger)

	go func() {
		addr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
		log.Printf("[server] DSPlus starting on %s", addr)
		log.Printf("[server] OpenAI upstream: %s", cfg.OpenAIUpstream)
		log.Printf("[server] Anthropic upstream: %s", cfg.AnthropicUpstream)

		if err := http.ListenAndServe(addr, proxy); err != nil {
			log.Fatalf("[server] failed to start: %v", err)
		}
	}()

	shutdownCh := make(chan struct{})
	if !*noGUI && cfg.AutoOpenGUI {
		go openGUI(fmt.Sprintf("http://127.0.0.1:%d", cfg.Port), shutdownCh)
	}

	fmt.Printf("DSPlus v0.1.0\n")
	fmt.Printf("Listening on http://127.0.0.1:%d\n", cfg.Port)
	fmt.Printf("GUI: http://127.0.0.1:%d\n", cfg.Port)
	fmt.Printf("Press Ctrl+C or close GUI window to stop\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
		log.Println("[server] shutting down via signal...")
	case <-shutdownCh:
		log.Println("[server] shutting down via GUI close...")
	}
}
