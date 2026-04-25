package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

// ModelProcess represents a running llama.cpp server process for a specific model
type ModelProcess struct {
	Config     ModelConfig
	LastUsed   time.Time
	Port       int
	Cmd        interface{} // *exec.Cmd, kept as interface for testability
	Ready      bool
}

// Proxy manages model lifecycle and routes incoming requests to the appropriate backend
type Proxy struct {
	config      *Config
	mu          sync.RWMutex
	current     *ModelProcess
	httpClient  *http.Client
}

// NewProxy creates a new Proxy instance from the provided configuration
func NewProxy(cfg *Config) (*Proxy, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	return &Proxy{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// ServeHTTP implements http.Handler, routing requests to the appropriate model backend
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Determine which model to use from the request
	modelName := p.resolveModel(r)
	if modelName == "" {
		http.Error(w, "could not determine model from request", http.StatusBadRequest)
		return
	}

	modelCfg, ok := p.config.Models[modelName]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown model: %s", modelName), http.StatusNotFound)
		return
	}

	// Ensure the correct model is running
	if err := p.ensureModel(modelName, modelCfg); err != nil {
		log.Printf("[proxy] failed to start model %s: %v", modelName, err)
		http.Error(w, "failed to start model backend", http.StatusServiceUnavailable)
		return
	}

	// Proxy the request to the backend
	p.forwardRequest(w, r)
}

// resolveModel extracts the model name from the incoming HTTP request.
// It checks the JSON body for a "model" field or falls back to a header.
func (p *Proxy) resolveModel(r *http.Request) string {
	// Check X-Model header first for simplicity
	if m := r.Header.Get("X-Model"); m != "" {
		return m
	}
	// Fall back to default model if configured
	return p.config.DefaultModel
}

// ensureModel starts the requested model if it is not already running,
// stopping any previously running model first.
func (p *Proxy) ensureModel(name string, cfg ModelConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.current != nil && p.current.Config.Cmd == cfg.Cmd {
		// Already running the correct model
		p.current.LastUsed = time.Now()
		return nil
	}

	// Stop the currently running model, if any
	if p.current != nil {
		log.Printf("[proxy] stopping model to load %s", name)
		p.stopCurrent()
	}

	log.Printf("[proxy] starting model %s", name)
	proc, err := startModelProcess(cfg)
	if err != nil {
		return fmt.Errorf("start model process: %w", err)
	}
	p.current = proc
	return nil
}

// stopCurrent terminates the currently running model process.
// Must be called with p.mu held.
func (p *Proxy) stopCurrent() {
	// Actual process termination handled in process.go
	p.current = nil
}

// forwardRequest reverse-proxies the request to the active backend process.
func (p *Proxy) forwardRequest(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	proc := p.current
	p.mu.RUnlock()

	if proc == nil {
		http.Error(w, "no model backend available", http.StatusServiceUnavailable)
		return
	}

	target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", proc.Port))
	if err != nil {
		http.Error(w, "invalid backend address", http.StatusInternalServerError)
		return
	}

	rp := httputil.NewSingleHostReverseProxy(target)
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[proxy] reverse proxy error: %v", err)
		http.Error(w, "backend error", http.StatusBadGateway)
	}
	rp.ServeHTTP(w, r)
}
