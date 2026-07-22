package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

var UNIX_PLUGIN_LISTENER = TEST_PREFIX + "/run/spr-krun-plugin/spr-dnscrypt.sock"

var gDaemon = NewDaemon()

func jsonResponse(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Println("encode failed:", err)
	}
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, gDaemon.Status())
}

func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	Configmtx.RLock()
	cfg := gConfig
	Configmtx.RUnlock()
	// no secrets in this config; safe to return as-is
	jsonResponse(w, cfg)
}

func handlePutConfig(w http.ResponseWriter, r *http.Request) {
	cfg := defaultConfig()
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	cfg, err := validateConfig(cfg)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	Configmtx.Lock()
	gConfig = cfg
	err = writeConfigLocked()
	Configmtx.Unlock()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// applied on next restart (POST /restart)
	jsonResponse(w, cfg)
}

func handleRestart(w http.ResponseWriter, r *http.Request) {
	if err := gDaemon.Restart(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonResponse(w, gDaemon.Status())
}

func handleResolvers(w http.ResponseWriter, r *http.Request) {
	resolvers, err := loadResolvers()
	if err != nil {
		http.Error(w, "failed to load resolvers list: "+err.Error(), 500)
		return
	}
	jsonResponse(w, resolvers)
}

// spaHandler serves the bundled UI (SPR fetches index.html over the unix
// socket and injects it as iframe srcDoc).
type spaHandler struct {
	staticPath string
	indexPath  string
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := filepath.Abs(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path = filepath.Join(h.staticPath, path)
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

func logRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL)
		handler.ServeHTTP(w, r)
	})
}

func main() {
	if err := loadConfig(); err != nil {
		fmt.Println("[-] no valid config, using defaults:", err)
	}

	if err := gDaemon.Start(); err != nil {
		// keep the API up so the UI can show the error and retry via /restart
		fmt.Println("[-] failed to start dnscrypt-proxy:", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", handleStatus)
	mux.HandleFunc("GET /config", handleGetConfig)
	mux.HandleFunc("PUT /config", handlePutConfig)
	mux.HandleFunc("POST /restart", handleRestart)
	mux.HandleFunc("GET /resolvers", handleResolvers)
	mux.Handle("/", spaHandler{staticPath: "/ui", indexPath: "index.html"})

	os.Remove(UNIX_PLUGIN_LISTENER)
	listener, err := net.Listen("unix", UNIX_PLUGIN_LISTENER)
	if err != nil {
		panic(err)
	}
	if err := os.Chmod(UNIX_PLUGIN_LISTENER, 0770); err != nil {
		fmt.Println("[-] chmod socket failed:", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		gDaemon.Stop()
		listener.Close()
		os.Remove(UNIX_PLUGIN_LISTENER)
		os.Exit(0)
	}()

	server := http.Server{Handler: logRequest(mux)}
	if err := server.Serve(listener); err != nil {
		log.Println(err)
	}
}
