package main // import "github.com/DrJosh9000/lifepo4wered-exporter"

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("listen-address", ":8080", "The address to listen on for HTTP requests.")

	lifepo4weredVars = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "lifepo4wered",
		Help: "Variables gathered from the lifepo4wered-cli tool",
	},
		[]string{"var"})
)

func init() {
	prometheus.MustRegister(lifepo4weredVars)
}

func getVars() {
	cmd := exec.Command("lifepo4wered-cli", "get")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Couldn't execute command: %v", err)
	}
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		// token space equals space number
		var name string
		var num int
		if _, err := fmt.Sscanf(s.Text(), "%s = %d", &name, &num); err != nil {
			log.Fatalf("Couldn't scan line: %v", err)
		}
		lifepo4weredVars.With(prometheus.Labels{"var": name}).Set(float64(num))
	}
	if err := s.Err(); err != nil {
		log.Fatalf("Couldn't scan output: %v", err)
	}
}

func main() {
	flag.Parse()

	getVars()
	go func() {
		for range time.Tick(15 * time.Second) {
			getVars()
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(*addr, nil))
}
