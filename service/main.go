package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	consulHost     = getenv("CONSUL_HOST", "consul")
	consulPort     = getenv("CONSUL_PORT", "8500")
	instanceName   = getenv("INSTANCE_NAME", "service-unknown")
	servicePort    = getenv("SERVICE_PORT", "5000")
	serviceName    = "hello-service"
	consulBase     = fmt.Sprintf("http://%s:%s", consulHost, consulPort)
	serviceAddress = instanceName // Docker Compose DNS — same as service name
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// --- Consul registration types ---

type consulCheck struct {
	HTTP                           string `json:"HTTP"`
	Interval                       string `json:"Interval"`
	Timeout                        string `json:"Timeout"`
	DeregisterCriticalServiceAfter string `json:"DeregisterCriticalServiceAfter"`
}

type consulRegistration struct {
	ID      string      `json:"ID"`
	Name    string      `json:"Name"`
	Address string      `json:"Address"`
	Port    int         `json:"Port"`
	Check   consulCheck `json:"Check"`
}

// --- Consul helpers ---

func registerWithConsul() error {
	port := 0
	fmt.Sscan(servicePort, &port)

	payload := consulRegistration{
		ID:      instanceName,
		Name:    serviceName,
		Address: serviceAddress,
		Port:    port,
		Check: consulCheck{
			HTTP:                           fmt.Sprintf("http://%s:%s/health", serviceAddress, servicePort),
			Interval:                       "10s",
			Timeout:                        "5s",
			DeregisterCriticalServiceAfter: "30s",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPut, consulBase+"/v1/agent/service/register", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("consul returned %d", resp.StatusCode)
	}

	log.Printf("Registered '%s' with Consul as '%s'", instanceName, serviceName)
	return nil
}

func deregisterFromConsul() {
	url := fmt.Sprintf("%s/v1/agent/service/deregister/%s", consulBase, instanceName)
	req, err := http.NewRequest(http.MethodPut, url, nil)
	if err != nil {
		log.Printf("Failed to build deregister request: %v", err)
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to deregister from Consul: %v", err)
		return
	}
	defer resp.Body.Close()
	log.Printf("Deregistered '%s' from Consul", instanceName)
}

// --- HTTP handlers ---

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"passing"}`))
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	port := 0
	fmt.Sscan(servicePort, &port)

	resp := map[string]interface{}{
		"message":      fmt.Sprintf("Hello from %s!", instanceName),
		"instance":     instanceName,
		"container_id": hostname,
		"port":         port,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// --- main ---

func main() {
	// Register with Consul — retry in case Consul isn't fully ready yet
	for i := 0; i < 5; i++ {
		if err := registerWithConsul(); err != nil {
			log.Printf("Consul registration attempt %d failed: %v. Retrying in 3s...", i+1, err)
			time.Sleep(3 * time.Second)
		} else {
			break
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/hello", helloHandler)

	srv := &http.Server{
		Addr:    "0.0.0.0:" + servicePort,
		Handler: mux,
	}

	// Listen for shutdown signals in background goroutine
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-quit
		log.Printf("Received signal %v. Shutting down gracefully...", sig)
		deregisterFromConsul()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	log.Printf("Service '%s' listening on :%s", instanceName, servicePort)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("ListenAndServe: %v", err)
	}
	log.Println("Server stopped cleanly.")
}
