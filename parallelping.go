// Ping multiple hosts in parallel

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	destinationFlag    string // Flag hosts: Comma-seperated list of IP/hostnames to ping
	pingCountFlag      uint64 // Flag count: Uint8 Interger number of pings to send per cycle.
	oneshotFlag        bool
	originFlag         string // String denoting specific origin hostname used in metric submission.
	intervalFlag       time.Duration
	verboseFlag        bool
	ipv6EnabledFlag    bool
	metricsPortFlag    uint64 // Port to listen in for metrics requests.
	re_ping_packetloss *regexp.Regexp
	re_ping_rtt        *regexp.Regexp
	re_ping_hostname   *regexp.Regexp
	quietFlag          bool

	pingBinary string // Path to ping binary

	ping_rtt_min_ms = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ping_rtt_min_ms",
			Help: "Ping rtt minimum in ms.",
		},
		[]string{
			"address_family",
			"destination",
			"hostname",
		},
	)
	ping_rtt_avg_ms = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ping_rtt_avg_ms",
			Help: "Ping rtt average in ms.",
		},
		[]string{
			"address_family",
			"destination",
			"hostname",
		},
	)
	ping_rtt_max_ms = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ping_rtt_max_ms",
			Help: "Ping rtt maximum in ms.",
		},
		[]string{
			"address_family",
			"destination",
			"hostname",
		},
	)
	ping_rtt_mdev_ms = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ping_rtt_mdev_ms",
			Help: "Ping rtt standard deviation in ms.",
		},
		[]string{
			"address_family",
			"destination",
			"hostname",
		},
	)
	ping_loss_pct = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ping_loss_pct",
			Help: "Ping rtt loss in percent.",
		},
		[]string{
			"address_family",
			"destination",
			"hostname",
		},
	)
)

type PingStats struct {
	loss float64
	min  float64
	avg  float64
	max  float64
	mdev float64
}

type Ping struct {
	origin         string
	destination    string
	hostname       string // Derived DNS hostname for destination.
	time           int64
	address_family string // ipv4, ipv6
	stats          PingStats
}

func getDistro() string {
	contents, _ := ioutil.ReadFile("/etc/os-release")
	re_distro := regexp.MustCompile("ID=([a-zA-Z]+)")

	str_contents := string(contents)
	if verboseFlag {
		log.Printf("Contents of /etc/os-release: %v.\n", str_contents)
	}
	distro := re_distro.FindStringSubmatch(str_contents)[1]

	if verboseFlag {
		log.Printf("Distribution determined to be: %v.\n", distro)
	}
	return distro
}

func setOsParams() {
	re_ping_hostname = regexp.MustCompile(`--- (?P<hostname>\S+) ping statistics ---`)
	if !quietFlag {
		log.Printf("Operating System determined to be: %v.\n", runtime.GOOS)
	}
	re_ping_packetloss = regexp.MustCompile(`(?P<loss>\d+)\% packet loss`)
	distro := getDistro()
	switch distro {
	case "ubuntu", "debian":
		pingBinary = "/usr/bin/ping"
		re_ping_rtt = regexp.MustCompile(`(rtt|round-trip) min/avg/max/(mdev|stddev) = (?P<min>\d+.\d+)/(?P<avg>\d+.\d+)/(?P<max>\d+.\d+)/(?P<mdev>\d+.\d+) ms`)
	case "alpine":
		pingBinary = "/bin/ping"
		re_ping_rtt = regexp.MustCompile(`round-trip min/avg/max = (?P<min>\d+.\d+)/(?P<avg>\d+.\d+)/(?P<max>\d+.\d+) ms`)
	}
}

func init() {
	flag.StringVar(&destinationFlag, "destination", "", "Comma-seperated list of destinations to ping.")
	flag.Uint64Var(&pingCountFlag, "pingcount", 5, "Number of pings per cycle.")
	flag.BoolVar(&oneshotFlag, "oneshot", false, "Execute just one ping round per host. Do not loop.")
	flag.StringVar(&originFlag, "origin", "", "Override hostname as origin with this value.")
	flag.DurationVar(&intervalFlag, "interval", 60*time.Second, "Seconds of wait in between each round of pings.")
	flag.BoolVar(&verboseFlag, "v", false, "If set, print out metrics as they are processed.")
	flag.BoolVar(&quietFlag, "q", false, "If set, only log in case of errors.")
	flag.BoolVar(&ipv6EnabledFlag, "ipv6", false, "If set, attempt to ping via IPv6 and gather statistics.")
	flag.Uint64Var(&metricsPortFlag, "metricsport", 9110, "Port to listen on for Prometheus metrics scrapes.")
}

// Return true if desination resolves, false if not.
func doesDestinationExist(d string) bool {
	addresses, _ := net.LookupHost(d)
	if len(addresses) > 0 {
		return true
	}
	return false
}

func getValidDestinations(d []string) []string {
	var vd []string
	for _, cd := range d {
		if doesDestinationExist(cd) {
			vd = append(vd, cd)
		}
	}
	return vd
}

func processPingOutput(pingOutput string) Ping {
	var ping Ping
	var stats PingStats
	now := time.Now()
	ping.time = now.Unix()
	if len(originFlag) == 0 {
		origin, _ := os.Hostname()
		ping.origin = origin
	} else {
		ping.origin = originFlag
	}

	re_ping_hostname_matches := re_ping_hostname.FindAllStringSubmatch(pingOutput, -1)[0]
	ping.hostname = re_ping_hostname_matches[1]

	re_packetloss_matches := re_ping_packetloss.FindAllStringSubmatch(pingOutput, -1)[0]

	stats.loss, _ = strconv.ParseFloat(re_packetloss_matches[1], 64)

	re_rtt_matches := re_ping_rtt.FindAllStringSubmatch(pingOutput, -1)[0]
	rtt_map := make(map[string]string)
	for i, name := range re_ping_rtt.SubexpNames() {
		if i != 0 {
			rtt_map[name] = re_rtt_matches[i]
		}
	}
	stats.min, _ = strconv.ParseFloat(rtt_map["min"], 64)
	stats.avg, _ = strconv.ParseFloat(rtt_map["avg"], 64)
	stats.max, _ = strconv.ParseFloat(rtt_map["max"], 64)
	stats.mdev, _ = strconv.ParseFloat(rtt_map["mdev"], 64)
	ping.stats = stats
	if verboseFlag {
		log.Printf("processPingOutput ping: %+v.\n", ping)
	}
	return ping
}

func executePing(host string, numPings uint64, do_ipv6 bool) (string, error) {
	countFlag := fmt.Sprintf("-c%v", numPings)
	var familyFlag string
	if do_ipv6 {
		familyFlag = "-6"
	} else {
		familyFlag = "-4"
	}
	pingCommandStr := pingBinary + " " + familyFlag + " " + countFlag + " " + host
	if verboseFlag {
		log.Printf("Ping Command: %s.\n", pingCommandStr)
	}
	pingCommand := strings.Fields(pingCommandStr)
	out, err := exec.Command(pingCommand[0], pingCommand[1:]...).Output()
	s_out := string(out[:])
	if verboseFlag {
		log.Printf("Raw Ping Output: %v\n", s_out)
	}
	if err != nil {
		log.Printf("Error with host %s, error: %s, output: %s\n", host, err, out)
		return s_out, err
	}
	return s_out, nil
}

func spawnPingLoop(c chan<- Ping,
	wg *sync.WaitGroup,
	destination string,
	numPings uint64,
	sleepTime time.Duration,
	oneshot bool,
	ipv6 bool) {
	for {
		log.Printf("Spawning IPv4 ping loop for %s.\n", destination)
		raw_output, err := executePing(destination, numPings, false)
		if err != nil {
			log.Printf("Error in executing IPv4 ping for %s.\n", destination)
		} else {
			pingResult := processPingOutput(raw_output)
			pingResult.destination = destination
			pingResult.address_family = "ipv4"
			c <- pingResult
		}

		if ipv6 {
			log.Printf("Spawning IPv6 ping loop for %s.\n", destination)
			raw_output, err := executePing(destination, numPings, true)
			if err != nil {
				log.Printf("Error in executing IPv6 ping for %s.\n", destination)
			} else {
				pingResult := processPingOutput(raw_output)
				pingResult.destination = destination
				pingResult.address_family = "ipv6"
				c <- pingResult
			}
		}
		if oneshot {
			wg.Done()
		} else {
			time.Sleep(sleepTime)
		}
	}
}

func updatePrometheusMetrics(ping Ping) {
	ping_rtt_min_ms.With(prometheus.Labels{
		"address_family": ping.address_family,
		"destination":    ping.destination,
		"hostname":       ping.hostname,
	}).Set(ping.stats.min)
	ping_rtt_avg_ms.With(prometheus.Labels{
		"address_family": ping.address_family,
		"destination":    ping.destination,
		"hostname":       ping.hostname,
	}).Set(ping.stats.avg)
	ping_rtt_max_ms.With(prometheus.Labels{
		"address_family": ping.address_family,
		"destination":    ping.destination,
		"hostname":       ping.hostname,
	}).Set(ping.stats.max)
	ping_rtt_mdev_ms.With(prometheus.Labels{
		"address_family": ping.address_family,
		"destination":    ping.destination,
		"hostname":       ping.hostname,
	}).Set(ping.stats.mdev)
	ping_loss_pct.With(prometheus.Labels{
		"address_family": ping.address_family,
		"destination":    ping.destination,
		"hostname":       ping.hostname,
	}).Set(ping.stats.loss)
}

func processPing(c <-chan Ping) {
	for {
		pr := <-c
		if verboseFlag {
			log.Printf("pingResult: %+v.\n", pr)
		}
		updatePrometheusMetrics(pr)
	}
}

func main() {
	flag.Parse()
	setOsParams()
	var wg sync.WaitGroup
	defer wg.Wait()

	destinations := strings.Split(destinationFlag, ",")
	validDestinations := getValidDestinations(destinations)
	log.Printf("Destinations to ping: %v.\n", validDestinations)
	var c chan Ping = make(chan Ping)
	for _, cd := range validDestinations {
		wg.Add(1)
		go func(dest string) {
			spawnPingLoop(c, &wg, dest, pingCountFlag, intervalFlag, oneshotFlag, ipv6EnabledFlag)
		}(cd)
	}

	go processPing(c)
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(fmt.Sprintf(":%d", metricsPortFlag), nil); err != nil {
			log.Fatal("HttpServer: ListenAndServe() error: " + err.Error())
		}
	}()
}
