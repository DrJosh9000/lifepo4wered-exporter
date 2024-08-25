// The lifepo4wered-exporter binary serves Prometheus metrics based on the output
package main // import "github.com/DrJosh9000/lifepo4wered-exporter"

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr         = flag.String("listen-address", ":9454", "The address to listen on for HTTP requests.")
	pollInterval = flag.Duration("poll-interval", 1*time.Second, "Time between executions of `lifepo4wered-cli get`")

	lifepo4weredVars = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "lifepo4wered",
			Help: "Variables gathered from the lifepo4wered-cli tool",
		},
		[]string{"var"},
	)

	lifepo4weredSummaries = map[string]*liteSummary{
		"VIN":  newLiteSummary("voltage_in", "Voltage in (mV)"),
		"VOUT": newLiteSummary("voltage_out", "Voltage out (mV)"),
		"VBAT": newLiteSummary("voltage_bat", "Battery voltage (mV)"),
		"IOUT": newLiteSummary("current_out", "Current out (mA)"),
	}
	lifepo4weredPOut = newLiteSummary("power_out", "Power out (mW)")
)

type liteSummary struct {
	mu    sync.Mutex
	min   float64
	max   float64
	sum   float64
	count int

	desc *prometheus.Desc
}

func newLiteSummary(varName, help string) *liteSummary {
	s := &liteSummary{
		min:  math.Inf(1),
		max:  math.Inf(-1),
		desc: prometheus.NewDesc("lifepo4wered_"+varName, help, []string{"stat"}, nil),
	}
	prometheus.MustRegister(s)
	return s
}

func (s *liteSummary) Describe(ch chan<- *prometheus.Desc) { ch <- s.desc }

func (s *liteSummary) Collect(ch chan<- prometheus.Metric) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch <- prometheus.MustNewConstMetric(s.desc, prometheus.GaugeValue, s.min, "min")
	ch <- prometheus.MustNewConstMetric(s.desc, prometheus.GaugeValue, s.max, "max")
	mean := s.sum
	if s.count > 0 {
		mean /= float64(s.count)
	}
	ch <- prometheus.MustNewConstMetric(s.desc, prometheus.GaugeValue, mean, "mean")
	s.min = math.Inf(1)
	s.max = math.Inf(-1)
	s.sum, s.count = 0, 0
}

func (s *liteSummary) Observe(x float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.min = min(s.min, x)
	s.max = max(s.max, x)
	s.sum += x
	s.count++
}

func pollVars() {
	cmd := exec.Command("lifepo4wered-cli", "get")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("Couldn't execute command: %v", err)
	}
	pout := 1
	labels := prometheus.Labels{"var": ""}

	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		// token space equals space number
		var name string
		var num int
		if _, err := fmt.Sscanf(s.Text(), "%s = %d", &name, &num); err != nil {
			log.Fatalf("Couldn't scan line: %v", err)
		}
		labels["var"] = name
		lifepo4weredVars.With(labels).Set(float64(num))

		v, ok := lifepo4weredSummaries[name]
		if !ok {
			continue
		}
		v.Observe(float64(num))
		switch name {
		case "VOUT", "IOUT":
			pout *= num
		}
	}
	if err := s.Err(); err != nil {
		log.Fatalf("Couldn't scan output: %v", err)
	}

	lifepo4weredPOut.Observe(float64(pout) / 1e3) // mV * mA = µW; 1000µW = 1mW.
}

func main() {
	flag.Parse()

	pollVars()
	go func() {
		for range time.Tick(*pollInterval) {
			pollVars()
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(*addr, nil))
}
