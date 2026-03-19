package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"
)

var (
	consulHost   = getenv("CONSUL_HOST", "consul")
	consulPort   = getenv("CONSUL_PORT", "8500")
	serviceName  = "hello-service"
	pollInterval = getDurationSeconds("POLL_INTERVAL", 2)
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDurationSeconds(key string, fallback float64) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return time.Duration(fallback * float64(time.Second))
	}
	var f float64
	fmt.Sscanf(v, "%f", &f)
	return time.Duration(f * float64(time.Second))
}

// --- Consul response types ---

type consulServiceEntry struct {
	Service struct {
		ID      string `json:"ID"`
		Address string `json:"Address"`
		Port    int    `json:"Port"`
	} `json:"Service"`
}

type instance struct {
	id      string
	address string
	port    int
}

// --- Discovery ---

func discoverInstances() []instance {
	url := fmt.Sprintf("http://%s:%s/v1/health/service/%s?passing",
		consulHost, consulPort, serviceName)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("Failed to query Consul: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Consul returned status %d", resp.StatusCode)
		return nil
	}

	var entries []consulServiceEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		log.Printf("Failed to decode Consul response: %v", err)
		return nil
	}

	instances := make([]instance, 0, len(entries))
	for _, e := range entries {
		instances = append(instances, instance{
			id:      e.Service.ID,
			address: e.Service.Address,
			port:    e.Service.Port,
		})
	}
	return instances
}

// --- Service call ---

func callService(inst instance) {
	url := fmt.Sprintf("http://%s:%d/hello", inst.address, inst.port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("[CALL -> %s] FAILED: %v", inst.id, err)
		return
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Printf("[CALL -> %s] Failed to decode response: %v", inst.id, err)
		return
	}

	log.Printf("[CALL -> %s]  message='%v'  container_id=%v",
		inst.id, data["message"], data["container_id"])
}

// --- main ---

func main() {
	log.Printf("Client starting. Discovering '%s' every %v. Press Ctrl-C to stop.",
		serviceName, pollInterval)

	callCount := 0
	for {
		instances := discoverInstances()

		if len(instances) == 0 {
			log.Println("No healthy instances found. Retrying...")
		} else {
			// Client-side load balancing: random selection from healthy instances
			chosen := instances[rand.Intn(len(instances))]
			callCount++
			log.Printf("[%d] Discovered %d instance(s). Picked: %s (%s:%d)",
				callCount, len(instances), chosen.id, chosen.address, chosen.port)
			callService(chosen)
		}

		time.Sleep(pollInterval)
	}
}
